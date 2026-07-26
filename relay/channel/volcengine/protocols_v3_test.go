package volcengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV3MessageRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		message Message
	}{
		{
			name: "start connection",
			message: Message{MsgType: MsgTypeFullClientRequest, MsgTypeFlag: MsgTypeFlagWithEvent,
				EventType: EventType_StartConnection, Payload: []byte("{}")},
		},
		{
			name: "start session",
			message: Message{MsgType: MsgTypeFullClientRequest, MsgTypeFlag: MsgTypeFlagWithEvent,
				EventType: EventType_StartSession, SessionID: "session-1", Payload: []byte(`{"text":"hello"}`)},
		},
		{
			name: "finish session",
			message: Message{MsgType: MsgTypeFullClientRequest, MsgTypeFlag: MsgTypeFlagWithEvent,
				EventType: EventType_FinishSession, SessionID: "session-1", Payload: []byte("{}")},
		},
		{
			name: "connection started",
			message: Message{MsgType: MsgTypeFullServerResponse, MsgTypeFlag: MsgTypeFlagWithEvent,
				EventType: EventType_ConnectionStarted, ConnectID: "connect-1", Payload: []byte("{}")},
		},
		{
			name: "connection finished",
			message: Message{MsgType: MsgTypeFullServerResponse, MsgTypeFlag: MsgTypeFlagWithEvent,
				EventType: EventType_ConnectionFinished, ConnectID: "connect-1", Payload: []byte("{}")},
		},
		{
			name: "audio with sequence",
			message: Message{MsgType: MsgTypeAudioOnlyServer, MsgTypeFlag: MsgTypeFlagPositiveSeq,
				Sequence: 7, Payload: []byte{1, 2, 3}},
		},
		{
			name: "provider error",
			message: Message{MsgType: MsgTypeError, MsgTypeFlag: MsgTypeFlagNoSeq,
				ErrorCode: 45000000, Payload: []byte(`{"message":"bad request"}`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.message
			source.Version = Version1
			source.HeaderSize = HeaderSize4
			source.Serialization = SerializationJSON
			source.Compression = CompressionNone

			frame, err := source.Marshal()
			require.NoError(t, err)
			parsed, err := NewMessageFromBytes(frame)
			require.NoError(t, err)

			assert.Equal(t, source.Version, parsed.Version)
			assert.Equal(t, source.HeaderSize, parsed.HeaderSize)
			assert.Equal(t, source.MsgType, parsed.MsgType)
			assert.Equal(t, source.MsgTypeFlag, parsed.MsgTypeFlag)
			assert.Equal(t, source.Serialization, parsed.Serialization)
			assert.Equal(t, source.Compression, parsed.Compression)
			assert.Equal(t, source.EventType, parsed.EventType)
			assert.Equal(t, source.SessionID, parsed.SessionID)
			assert.Equal(t, source.ConnectID, parsed.ConnectID)
			assert.Equal(t, source.Sequence, parsed.Sequence)
			assert.Equal(t, source.ErrorCode, parsed.ErrorCode)
			assert.Equal(t, source.Payload, parsed.Payload)
		})
	}
}

func TestConnectionFinishedRoundTripKeepsConnectionFieldsSymmetric(t *testing.T) {
	source := Message{
		Version:       Version1,
		HeaderSize:    HeaderSize4,
		MsgType:       MsgTypeFullServerResponse,
		MsgTypeFlag:   MsgTypeFlagWithEvent,
		Serialization: SerializationJSON,
		Compression:   CompressionNone,
		EventType:     EventType_ConnectionFinished,
		ConnectID:     "connection-finished-id",
		Payload:       []byte("{}"),
	}

	frame, err := source.Marshal()
	require.NoError(t, err)
	parsed, err := NewMessageFromBytes(frame)
	require.NoError(t, err)

	assert.Empty(t, parsed.SessionID)
	assert.Equal(t, source.ConnectID, parsed.ConnectID)
}
