package evr

import (
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/lo"
)

func TestLobbyCreateSessionRequest_Unmarshal(t *testing.T) {
	var err error

	data, err := WrapBytes(SymbolOf(&LobbyCreateSessionRequest{}), []byte{
		0x3a, 0xa0, 0x23, 0x12, 0xb2, 0xe7, 0x5f, 0x45,
		0x0d, 0x91, 0x77, 0x8f, 0xd7, 0x01, 0x2f, 0xc6,
		0x03, 0x8c, 0xdb, 0xf4, 0x65, 0x09, 0x99, 0x09,
		0x4b, 0xbc, 0x8e, 0x42, 0xf8, 0xd3, 0x6e, 0x57,
		0xf8, 0xf4, 0x9f, 0xa8, 0xb1, 0xd0, 0xe8, 0xc8,
		0xab, 0xed, 0x0c, 0x32, 0x50, 0xfb, 0xee, 0x11,
		0x8e, 0x45, 0x66, 0xd3, 0xff, 0x8a, 0x65, 0x3b,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00, 0x0b, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x7b, 0x22, 0x67, 0x61, 0x6d, 0x65, 0x74, 0x79,
		0x70, 0x65, 0x22, 0x3a, 0x36, 0x39, 0x31, 0x35,
		0x39, 0x34, 0x33, 0x35, 0x31, 0x32, 0x38, 0x32,
		0x34, 0x35, 0x37, 0x36, 0x30, 0x33, 0x2c, 0x22,
		0x61, 0x70, 0x70, 0x69, 0x64, 0x22, 0x3a, 0x22,
		0x31, 0x33, 0x36, 0x39, 0x30, 0x37, 0x38, 0x34,
		0x30, 0x39, 0x38, 0x37, 0x33, 0x34, 0x30, 0x32,
		0x22, 0x7d, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x16, 0xa9, 0x53, 0x29, 0xef,
		0x14, 0x0e, 0x00, 0x02, 0x00, 0x0a,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add the header to the payload

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := packet[0].(*LobbyCreateSessionRequest)
	if !ok {
		t.Fatal("failed to cast")
	}

	want := LobbyCreateSessionRequest{
		LobbyType:      uint32(PrivateLobby),
		Region:         4998968863399059514,
		VersionLock:    -4166109104957845235,
		Mode:           ModeArenaPrivate,
		Level:          6300205991959903307,
		Platform:       14477050463639303416,
		LoginSessionID: uuid.Must(uuid.FromString("320cedab-fb50-11ee-8e45-66d3ff8a653b")),
		Unk1:           1,
		Unk2:           11,
		SessionSettings: SessionSettings{
			AppID: "1369078409873402",
			Mode:  691594351282457603,
			Level: nil,
		},
		EvrId:     *lo.Must(ParseEvrId("OVR_ORG-3963667097037078")),
		TeamIndex: 2,
	}

	if *got != want {
		t.Errorf("\ngot  %s\nwant %s", got.String(), want.String())
	}

}
func TestLobbyCreateSessionRequest_GameType(t *testing.T) {
	var err error

	/* args were "-noovr -novr -gametype echo_arena_private  -lobbyteam 2 -mp" */

	// It's setting the server region to the same value as the level

	data, err := WrapBytes(SymbolOf(&LobbyCreateSessionRequest{}), []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0x0d, 0x91, 0x77, 0x8f, 0xd7, 0x01, 0x2f, 0xc6,
		0x03, 0x8c, 0xdb, 0xf4, 0x65, 0x09, 0x99, 0x09,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xf8, 0xf4, 0x9f, 0xa8, 0xb1, 0xd0, 0xe8, 0xc8,
		0x62, 0xa1, 0x19, 0x5f, 0xb2, 0xfb, 0xee, 0x11,
		0xa9, 0x46, 0x66, 0xd3, 0xff, 0x8a, 0x65, 0x3b,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x7b, 0x22, 0x67, 0x61, 0x6d, 0x65, 0x74, 0x79,
		0x70, 0x65, 0x22, 0x3a, 0x36, 0x39, 0x31, 0x35,
		0x39, 0x34, 0x33, 0x35, 0x31, 0x32, 0x38, 0x32,
		0x34, 0x35, 0x37, 0x36, 0x30, 0x33, 0x2c, 0x22,
		0x61, 0x70, 0x70, 0x69, 0x64, 0x22, 0x3a, 0x22,
		0x31, 0x33, 0x36, 0x39, 0x30, 0x37, 0x38, 0x34,
		0x30, 0x39, 0x38, 0x37, 0x33, 0x34, 0x30, 0x32,
		0x22, 0x7d, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x16, 0xa9, 0x53, 0x29, 0xef,
		0x14, 0x0e, 0x00, 0x0a,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add the header to the payload

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := packet[0].(*LobbyCreateSessionRequest)
	if !ok {
		t.Fatal("failed to cast")
	}

	want := LobbyCreateSessionRequest{
		LobbyType:      uint32(PrivateLobby),
		Region:         4998968863399059514,
		VersionLock:    -4166109104957845235,
		Mode:           ModeArenaPrivate,
		Level:          6300205991959903307,
		Platform:       14477050463639303416,
		LoginSessionID: uuid.Must(uuid.FromString("320cedab-fb50-11ee-8e45-66d3ff8a653b")),
		Unk1:           1,
		Unk2:           11,
		SessionSettings: SessionSettings{
			AppID: "1369078409873402",
			Mode:  691594351282457603,
			Level: nil,
		},
		EvrId:     *lo.Must(ParseEvrId("OVR_ORG-3963667097037078")),
		TeamIndex: 2,
	}

	if *got != want {
		t.Errorf("\ngot  %s\nwant %s", got.String(), want.String())
	}

}
