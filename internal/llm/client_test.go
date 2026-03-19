package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openai/openai-go/v3/responses"
)

func TestNewClientValidation(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		apiKey   string
		model    string
	}{
		{name: "empty endpoint", endpoint: "", apiKey: "key", model: "model"},
		{name: "empty api key", endpoint: "https://example.com", apiKey: "", model: "model"},
		{name: "empty model", endpoint: "https://example.com", apiKey: "key", model: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(tc.endpoint, tc.apiKey, tc.model)
			require.Error(t, err)
			require.Nil(t, client)
		})
	}
}

func TestNewClientSuccess(t *testing.T) {
	client, err := NewClient("https://example.com", "key", "model")
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestCreateResponseEmptyInput(t *testing.T) {
	client, err := NewClient("https://example.com", "key", "model")
	require.NoError(t, err)
	_, err = client.CreateResponse(context.Background(), "", nil, nil, responses.ResponseNewParamsToolChoiceUnion{}, false, nil)
	require.Error(t, err)
}
