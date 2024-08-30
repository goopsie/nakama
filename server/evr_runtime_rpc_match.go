package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/heroiclabs/nakama-common/runtime"
)

type shutdownMatchRequest struct {
	MatchID      MatchID `json:"match_id"`
	GraceSeconds int     `json:"grace_seconds,omitempty"`
}

type shutdownMatchResponse struct {
	Success  bool   `json:"success"`
	Response string `json:"response"`
}

func shutdownMatchRpc(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	request := &shutdownMatchRequest{}
	if err := json.Unmarshal([]byte(payload), request); err != nil {
		return "", err
	}

	label, err := MatchLabelByID(ctx, nk, request.MatchID)
	if err != nil {
		return "", err
	}

	if label.LobbyType == UnassignedLobby {
		return "", fmt.Errorf("match %s is not in a lobby", request.MatchID)
	}
	if request.GraceSeconds <= 0 {
		request.GraceSeconds = 10
	}

	signalMessage := &SignalShutdownPayload{
		GraceSeconds: request.GraceSeconds,
	}

	signalJson, err := json.Marshal(signalMessage)
	if err != nil {
		return "", err
	}
	signalResponse, err := nk.MatchSignal(ctx, request.MatchID.String(), string(signalJson))
	if err != nil {
		return "", err
	}

	response := &shutdownMatchResponse{
		Success:  true,
		Response: signalResponse,
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		return "", err
	}

	return string(jsonData), nil
}
