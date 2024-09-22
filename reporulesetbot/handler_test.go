package reporulesetbot

import (
	"context"
	"testing"

	"github.com/google/go-github/v65/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockClientCreator is a mock implementation of githubapp.ClientCreator.
type MockClient struct {
	mock.Mock
	githubapp.ClientCreator
}

func (m *MockClient) NewInstallationClient(installationID int64) (*github.Client, error) {
	args := m.Called(installationID)
	return args.Get(0).(*github.Client), args.Error(1)
}

func TestHandles(t *testing.T) {
	handler := &RulesetHandler{}
	expected := []string{"repository_ruleset", "installation", "release"}
	assert.Equal(t, expected, handler.Handles())
}

func TestHandle(t *testing.T) {
	mockClient := new(MockClient)
	handler := &RulesetHandler{
		ClientCreator: mockClient,
	}

	mockClient.On("NewInstallationClient", int64(1)).Return(&github.Client{}, nil)

	ctx := context.Background()
	deliveryID := "test-delivery-id"
	//validInstallationPayload := []byte(`{"action": "created", "installation": {"id": 1, "account": {"login": "test-org"}}}`)
	validRulesetPayload := []byte(`{"action": "created", "repository_ruleset":{"id": 1, "name": "test-ruleset", "enforcement": "disabled"}}`)
	//validReleasePayload := []byte(`{"action": "released"}`)
	invalidPayload := []byte(`not a valid payload`)

	t.Run("repository_ruleset event", func(t *testing.T) {
		err := handler.Handle(ctx, "repository_ruleset", deliveryID, validRulesetPayload)
		assert.NoError(t, err)
	})

	t.Run("unhandled event", func(t *testing.T) {
		err := handler.Handle(ctx, "unknown_event", deliveryID, invalidPayload)
		assert.NoError(t, err)
	})
}
