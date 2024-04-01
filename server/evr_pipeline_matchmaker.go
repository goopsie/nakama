package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// lobbyMatchmakerStatusRequest is a message requesting the status of the matchmaker.
func (p *EvrPipeline) lobbyMatchmakerStatusRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	_ = in.(*evr.LobbyMatchmakerStatusRequest)

	// TODO Check if the matchmaking ticket is still open
	err := session.SendEvr([]evr.Message{
		evr.NewLobbyMatchmakerStatusResponse(),
	})
	if err != nil {
		return fmt.Errorf("LobbyMatchmakerStatus: %v", err)
	}
	return nil
}

// authorizeMatchmaking checks if the user is allowed to join a public match or spawn a new match
func (p *EvrPipeline) authorizeMatchmaking(ctx context.Context, logger *zap.Logger, session *sessionWS, channel uuid.UUID) (bool, error) {

	// Get the EvrID from the context
	evrId, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		return false, fmt.Errorf("failed to get evrID from context")
	}
	if !evrId.Valid() {
		return false, fmt.Errorf("evrID is invalid")
	}

	// Send a match leave if this user is in another match
	if session.userID == uuid.Nil {
		return false, status.Errorf(codes.PermissionDenied, "User not authenticated")
	}
	//evrModes := map[uint8]struct{}{StreamModeEvr: {}, StreamModeMatchAuthoritative: {}}
	//stream := PresenceStream{Mode: StreamModeEvr, Subject: session.userID, Subcontext: svcMatchID, Label: p.node}
	sessionIDs := session.tracker.ListLocalSessionIDByStream(PresenceStream{Mode: StreamModeEvr, Subject: session.userID, Subcontext: svcMatchID})
	for _, foundSessionID := range sessionIDs {
		if foundSessionID == session.id {
			// Allow the current session, only disconnect any older ones.
			continue
		}

		// Disconnect the older session.
		logger.Debug("Disconnecting older session from matchmaking", zap.String("other_sid", foundSessionID.String()))
		fs := p.sessionRegistry.Get(foundSessionID)
		if fs == nil {
			logger.Warn("Failed to find older session to disconnect", zap.String("other_sid", foundSessionID.String()))
			continue
		}
		fs.Close("New session started", runtime.PresenceReasonDisconnect)
	}

	// Track this session as a matchmaking session.
	s := session
	s.tracker.TrackMulti(s.ctx, s.id, []*TrackerOp{
		// EVR packet data stream for the match session by userID, and service ID
		{
			Stream: PresenceStream{Mode: StreamModeEvr, Subject: s.userID, Subcontext: svcMatchID},
			Meta:   PresenceMeta{Format: s.format, Hidden: true},
		},
		// EVR packet data stream for the match session by Session ID and service ID
		{
			Stream: PresenceStream{Mode: StreamModeEvr, Subject: s.id, Subcontext: svcMatchID},
			Meta:   PresenceMeta{Format: s.format, Hidden: true},
		},
	}, s.userID, true)

	if channel == uuid.Nil {
		logger.Warn("Channel is nil")
		return true, nil
	}
	// Check for suspensions on this channel.
	suspensions, err := p.checkSuspensionStatus(ctx, logger, session.UserID().String(), channel)
	if err != nil {
		return true, status.Errorf(codes.Internal, "Failed to check suspension status: %v", err)
	}
	if len(suspensions) != 0 {
		msg := suspensions[0].Reason

		return false, status.Errorf(codes.PermissionDenied, msg)
	}
	return true, nil
}
func (p *EvrPipeline) matchmakingLabelFromFindRequest(ctx context.Context, session *sessionWS, request *evr.LobbyFindSessionRequest) (*EvrMatchState, error) {
	// If the channel is nil, use the players profile channel
	channel := request.Channel
	if channel == uuid.Nil {
		profile := p.profileRegistry.GetProfile(session.userID)
		if profile == nil {
			return nil, status.Errorf(codes.Internal, "Failed to get players profile")
		}
		channel = profile.GetChannel()
	}

	// Set the channels this player is allowed to matchmake/create a match on.
	groups, err := p.discordRegistry.GetGuildGroups(ctx, session.UserID())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get guild groups: %v", err)
	}

	allowedChannels := make([]uuid.UUID, 0, len(groups))
	for _, group := range groups {
		allowedChannels = append(allowedChannels, uuid.FromStringOrNil(group.Id))
	}

	return &EvrMatchState{
		Channel: &channel,

		MatchID: request.MatchingSession, // The existing lobby/match that the player is in (if any)
		Mode:    request.Mode,
		Level:   request.Level,
		Open:    true,

		SessionSettings: &request.SessionSettings,
		TeamIndex:       TeamIndex(request.TeamIndex),

		Broadcaster: MatchBroadcaster{
			VersionLock: request.VersionLock,
			Channels:    allowedChannels,
		},
	}, nil
}

// lobbyFindSessionRequest is a message requesting to find a public session to join.
func (p *EvrPipeline) lobbyFindSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) (err error) {
	request := in.(*evr.LobbyFindSessionRequest)

	groups, priorities, err := p.GetGuildPriorityList(ctx, session.userID)
	if err != nil {
		logger.Warn("Failed to get guild priority list", zap.Error(err))
	}

	// Validate that the channel is in the user's guilds
	if request.Channel == uuid.Nil || !lo.Contains(groups, request.Channel) {
		if len(priorities) > 0 {
			// If the channel is nil, use the players primary channel
			request.Channel = priorities[0]
		} else if len(groups) > 0 {
			// If the player has no guilds, use the first guild
			request.Channel = groups[0]
		} else {
			// If the player has no guilds,
			return NewMatchmakingResponse(request.Mode, uuid.Nil).SendErrorToSession(session, status.Errorf(codes.PermissionDenied, "No guilds available"))
		}
	}

	response := NewMatchmakingResponse(request.Mode, request.Channel)

	// Build the matchmaking label using the request parameters
	ml, err := p.matchmakingLabelFromFindRequest(ctx, session, request)
	if err != nil {
		return response.SendErrorToSession(session, err)
	}

	// TODO Check if the user is in a party.

	// Check for suspensions on this channel, if this is a request for a public match.
	if authorized, err := p.authorizeMatchmaking(ctx, logger, session, *ml.Channel); !authorized {
		return response.SendErrorToSession(session, err)
	} else if err != nil {
		logger.Warn("Failed to authorize matchmaking, allowing player to continue. ", zap.Error(err))
	}

	// Wait for a graceperiod Unless this is a social lobby, wait for a grace period before starting the matchmaker
	matchmakingDelay := MatchJoinGracePeriod
	if ml.Mode == evr.ModeSocialPublic {
		matchmakingDelay = 0
	}
	select {
	case <-time.After(matchmakingDelay):
	case <-ctx.Done():
		return nil
	}

	go func() {
		// Create the matchmaking session
		err = p.MatchFind(ctx, session, ml)
		if err != nil {
			response.SendErrorToSession(session, err)
		}
	}()

	return nil
}

func (p *EvrPipeline) MatchBackfillLoop(session *sessionWS, msession *MatchmakingSession, skipDelay bool) error {
	interval := p.config.GetMatchmaker().IntervalSec
	idealMatchIntervals := p.config.GetMatchmaker().RevThreshold

	ctx := msession.Context()
	// Wait for at least 1 interval before starting to look for a backfill.
	// This gives the matchmaker a chance to find a full ideal match
	backfillDelay := time.Duration(interval*idealMatchIntervals) * time.Second
	if skipDelay {
		backfillDelay = 5 * time.Second
	}

	// Check for a backfill match on a regular basis
	backfilInterval := time.Duration(5) * time.Second
	backfillTicker := time.NewTicker(backfilInterval)

	<-time.After(backfillDelay)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Backfill any existing matches
			label, err := p.Backfill(ctx, session, msession)
			if err != nil {
				return msession.Cancel(fmt.Errorf("failed to find backfill match: %w", err))
			}
			if label != nil {
				msession.MatchIdCh <- label.MatchID.String()
			}
		}
		<-backfillTicker.C
	}
}

func (p *EvrPipeline) MatchCreateLoop(session *sessionWS, msession *MatchmakingSession, pruneDelay time.Duration) error {
	ctx := msession.Context()
	// set a timeout
	//stageTimer := time.NewTimer(pruneDelay)
	for {

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		// Stage 1: Check if there is an available broadcaster
		matchID, err := p.MatchCreate(ctx, session, msession, msession.Label)

		switch status.Code(err) {

		case codes.OK:
			if matchID == "" {
				return msession.Cancel(fmt.Errorf("match is nil"))
			}

			msession.MatchIdCh <- matchID
			return nil

		case codes.NotFound, codes.ResourceExhausted, codes.Unavailable:
			// All the servers are being used.
			<-time.After(5 * time.Second)
			/*
				select {

				case <-time.After(5 * time.Second):
					// Wait 5 seconds before trying again.
					continue

				case <-stageTimer.C:
					// Move Stage 2: Kill a private lobby with only 1-2 people in it.
					err := p.pruneMatches(ctx, session)
					if err != nil {
						return msession.Cancel(err)
					}
				}
			*/
		default:
			return msession.Cancel(err)
		}
	}
}

func (p *EvrPipeline) MatchFind(parentCtx context.Context, session *sessionWS, ml *EvrMatchState) error {
	if s, found := p.matchmakingRegistry.GetMatchingBySessionId(session.id); found {
		// Replace the session
		session.logger.Error("Matchmaking session already exists", zap.Any("tickets", s.Tickets))
	}

	joinFn := func(matchID string) error {
		return p.JoinEvrMatch(parentCtx, session, matchID, *ml.Channel, int(ml.TeamIndex))
	}
	errorFn := func(err error) error {
		return NewMatchmakingResponse(ml.Mode, *ml.Channel).SendErrorToSession(session, err)
	}

	// Create a new matching session
	session.logger.Debug("Creating a new matchmaking session")
	partySize := 1
	timeout := 60 * time.Minute
	pruneDelay := 5 * time.Minute // The amount of time to wait before pruning solo private matches

	if ml.TeamIndex == TeamIndex(evr.TeamSpectator) {
		timeout = 12 * time.Hour
	}

	msession, err := p.matchmakingRegistry.Create(parentCtx, session, ml, partySize, timeout, errorFn, joinFn)
	if err != nil {
		session.logger.Error("Failed to create matchmaking session", zap.Error(err))
		return err
	}

	logger := msession.Logger

	skipBackfillDelay := false
	if ml.TeamIndex == TeamIndex(evr.TeamSpectator) || ml.TeamIndex == TeamIndex(evr.TeamModerator) {
		skipBackfillDelay = true
		if ml.Mode != evr.ModeArenaPublic && ml.Mode != evr.ModeCombatPublic {
			return fmt.Errorf("spectators are only allowed in arena and combat matches")
		}
		// Spectators don't matchmake, and they don't have a delay for backfill.
		// Spectators also don't time out.
		go p.MatchBackfillLoop(session, msession, skipBackfillDelay)
		return nil
	}

	switch ml.Mode {
	// For public matches, backfill or matchmake
	// If it's a social match, backfill or create immediately
	case evr.ModeSocialPublic:
		// Backfill any existing matches or create one
		label, err := p.Backfill(msession.Context(), session, msession)
		if err != nil {
			logger.Error("Failed to find backfill match", zap.Error(err))
			return err
		}
		if label != nil {
			msession.MatchIdCh <- label.MatchID.String()
			return nil
		} else {
			// Continue to try to backfill
			go p.MatchBackfillLoop(session, msession, skipBackfillDelay)
			// While also trying to create a match
			go p.MatchCreateLoop(session, msession, pruneDelay)
		}
		// For public arena/combat matches, backfill while matchmaking
	case evr.ModeCombatPublic:
		// Join any on-going combat match without delay
		skipBackfillDelay = false
		// For Arena and combat matches try to backfill while matchmaking
		fallthrough
	case evr.ModeArenaPublic:

		// Start the backfill loop
		go p.MatchBackfillLoop(session, msession, skipBackfillDelay)

		// Put a ticket in for matching
		_, err := p.MatchMake(session, msession)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown mode: %v", ml.Mode)
	}

	return nil
}

// lobbyPingResponse is a message responding to a ping request.
func (p *EvrPipeline) lobbyPingResponse(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	response := in.(*evr.LobbyPingResponse)

	userID := session.UserID()
	// Validate the connection.
	if userID == uuid.Nil {
		return fmt.Errorf("session not authenticated")
	}

	// Look up the matching session.
	msession, ok := p.matchmakingRegistry.GetMatchingBySessionId(session.id)
	if !ok {
		return fmt.Errorf("matching session not found")
	}
	// Send a signal to the matching session to continue searching.
	msession.PingResultsCh <- response.Results

	return nil
}

func (p *EvrPipeline) GetGuildPriorityList(ctx context.Context, userID uuid.UUID) (all []uuid.UUID, selected []uuid.UUID, err error) {

	profile := p.profileRegistry.GetProfile(userID)
	if profile == nil {
		return nil, nil, status.Errorf(codes.Internal, "Failed to get players profile")
	}
	currentChannel := profile.GetChannel()

	// Get the guild priority from the context
	groups, err := p.discordRegistry.GetGuildGroups(ctx, userID)
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "Failed to get guilds: %v", err)
	}

	groupIDs := make([]uuid.UUID, 0)
	for _, group := range groups {
		groupIDs = append(groupIDs, uuid.FromStringOrNil(group.Id))
	}

	guildPriority := make([]uuid.UUID, 0)
	params, ok := ctx.Value(ctxUrlParamsKey{}).(map[string][]string)
	if ok {
		// If the params are set, use them
		for _, gid := range params["guilds"] {
			for _, guildId := range strings.Split(gid, ",") {
				s := strings.Trim(guildId, " ")
				if s != "" {
					// Get the groupId for the guild
					groupIDstr, found := p.discordRegistry.Get(s)
					if !found {
						continue
					}
					groupID := uuid.FromStringOrNil(groupIDstr)
					if groupID != uuid.Nil && lo.Contains(groupIDs, groupID) {
						guildPriority = append(guildPriority, groupID)
					}
				}
			}
		}
	} else {
		// If the params are not set, use the user's guilds
		guildPriority = []uuid.UUID{currentChannel}
		for _, groupID := range groupIDs {
			if groupID != currentChannel {
				guildPriority = append(guildPriority, groupID)
			}
		}
	}

	return groupIDs, selected, nil
}

// lobbyCreateSessionRequest is a request to create a new session.
func (p *EvrPipeline) lobbyCreateSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.LobbyCreateSessionRequest)

	groups, priorities, err := p.GetGuildPriorityList(ctx, session.userID)
	if err != nil {
		logger.Warn("Failed to get guild priority list", zap.Error(err))
	}

	result := NewMatchmakingResponse(request.Mode, request.Channel)

	// Validate that the channel is in the user's guilds
	if request.Channel == uuid.Nil || !lo.Contains(groups, request.Channel) {
		if len(priorities) > 0 {
			// If the channel is nil, use the players primary channel
			request.Channel = priorities[0]
		} else if len(groups) > 0 {
			// If the player has no guilds, use the first guild
			request.Channel = groups[0]
		} else {
			// If the player has no guilds,
			return NewMatchmakingResponse(request.Mode, uuid.Nil).SendErrorToSession(session, status.Errorf(codes.PermissionDenied, "No guilds available"))
		}

	}

	// Check for suspensions on this channel. The user will not be allowed to create lobby's
	if authorized, err := p.authorizeMatchmaking(ctx, logger, session, request.Channel); !authorized {
		return result.SendErrorToSession(session, err)
	} else if err != nil {
		logger.Warn("Failed to authorize matchmaking, allowing player to continue. ", zap.Error(err))
	}
	ml := &EvrMatchState{

		Level:           request.Level,
		LobbyType:       LobbyType(request.LobbyType),
		Mode:            request.Mode,
		Open:            true,
		SessionSettings: &request.SessionSettings,
		TeamIndex:       TeamIndex(request.TeamIndex),
		Channel:         &request.Channel,
		Broadcaster: MatchBroadcaster{
			VersionLock: uint64(request.VersionLock),
			Region:      request.Region,
			Channels:    priorities,
		},
	}

	// Start the search in a goroutine.
	go func() error {
		// Set some defaults
		partySize := 1 // TODO FIXME this should include the party size

		joinFn := func(matchID string) error {
			return p.JoinEvrMatch(ctx, session, matchID, *ml.Channel, int(ml.TeamIndex))
		}

		errorFn := func(err error) error {
			return NewMatchmakingResponse(ml.Mode, *ml.Channel).SendErrorToSession(session, err)
		}

		// Create a matching session
		timeout := 15 * time.Minute
		msession, err := p.matchmakingRegistry.Create(ctx, session, ml, partySize, timeout, errorFn, joinFn)
		if err != nil {
			return result.SendErrorToSession(session, status.Errorf(codes.Internal, "Failed to create matchmaking session: %v", err))
		}

		// Create a new match
		matchID, err := p.MatchCreate(ctx, session, msession, ml)
		if err != nil {
			return result.SendErrorToSession(session, err)
		}

		err = p.JoinEvrMatch(msession.Ctx, session, matchID, request.Channel, int(ml.TeamIndex))
		if err != nil {
			return result.SendErrorToSession(session, err)
		}
		return nil
	}()

	return nil
}

// lobbyJoinSessionRequest is a request to join a specific existing session.
func (p *EvrPipeline) lobbyJoinSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.LobbyJoinSessionRequest)

	// Make sure the match exists
	matchId := request.LobbyId.String() + "." + p.node
	match, _, err := p.matchRegistry.GetMatch(ctx, matchId)
	if err != nil {
		return NewMatchmakingResponse(0, uuid.Nil).SendErrorToSession(session, err)
	}
	if match == nil {
		return NewMatchmakingResponse(0, uuid.Nil).SendErrorToSession(session, status.Errorf(codes.NotFound, "Match not found"))
	}

	// Extract the label
	ml := &EvrMatchState{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), ml); err != nil {
		return NewMatchmakingResponse(0, uuid.Nil).SendErrorToSession(session, err)
	}

	// Check if the match is a lobby (and not a parking match)
	if ml.LobbyType == UnassignedLobby {
		return NewMatchmakingResponse(0, uuid.Nil).SendErrorToSession(session, status.Errorf(codes.InvalidArgument, "Match is not a lobby"))
	}

	if ml.Channel == nil {
		ml.Channel = &uuid.Nil
	}

	result := NewMatchmakingResponse(ml.Mode, *ml.Channel)
	// Check for suspensions on this channel.
	if ml.Mode == evr.ModeArenaPublic || ml.Mode == evr.ModeCombatPublic || ml.Mode == evr.ModeSocialPublic {
		// Check for suspensions on this channel, if this is a request for a public match.
		if authorized, err := p.authorizeMatchmaking(ctx, logger, session, *ml.Channel); !authorized {
			return result.SendErrorToSession(session, err)
		} else if err != nil {
			logger.Warn("Failed to authorize matchmaking, allowing player to continue. ", zap.Error(err))
		}
	}

	// Join the match
	if err = p.JoinEvrMatch(ctx, session, matchId, *ml.Channel, int(ml.TeamIndex)); err != nil {
		return result.SendErrorToSession(session, err)
	}
	return nil
}

// LobbyPendingSessionCancel is a message from the server to the client, indicating that the user wishes to cancel matchmaking.
func (p *EvrPipeline) lobbyPendingSessionCancel(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	// Look up the matching session.
	if matchingSession, ok := p.matchmakingRegistry.GetMatchingBySessionId(session.id); ok {
		matchingSession.Cancel(ErrMatchmakingCancelledByPlayer)
	}
	return nil
}

// pruneMatches prunes matches that are underutilized
func (p *EvrPipeline) pruneMatches(ctx context.Context, session *sessionWS) error {
	session.logger.Warn("Pruning matches")
	matches, err := p.matchmakingRegistry.listMatches(ctx, 1000, 2, 3, "*")
	if err != nil {
		return err
	}

	for _, match := range matches {
		_, err := SignalMatch(ctx, p.matchRegistry, match.MatchId, SignalPruneUnderutilized, nil)
		if err != nil {
			return err

		}
	}
	return nil
}
