package reporulesetbot

import (
	"os"
	"testing"

	"github.com/google/go-github/v65/github"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestShouldProcessBypassActor(t *testing.T) {
	bypassActor := &github.BypassActor{}
	bypassActor.ActorID = github.Int64(6)
	assert.True(t, shouldProcessBypassActor(bypassActor))

	bypassActor.ActorID = github.Int64(4)
	assert.False(t, shouldProcessBypassActor(bypassActor))
}

func TestIsManagedRuleset(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	tests := []struct {
		name     string
		event    *RulesetEvent
		ruleset  *github.Ruleset
		expected bool
	}{
		{
			name: "ruleset name changed",
			event: &RulesetEvent{
				Ruleset: &github.Ruleset{
					Name: "unmanaged-ruleset",
				},
				Changes: &Changes{
					Name: struct {
						From string `json:"from,omitempty"`
					}{
						From: "managed-ruleset",
					},
				},
				Organization: &github.Organization{
					Login: github.String("test-org"),
				},
			},
			ruleset: &github.Ruleset{
				Name: "managed-ruleset",
			},
			expected: true,
		},
		{
			name: "ruleset name not managed by app",
			event: &RulesetEvent{
				Ruleset: &github.Ruleset{
					Name: "unmanaged-ruleset",
				},
				Changes: &Changes{},
				Organization: &github.Organization{
					Login: github.String("test-org"),
				},
			},
			ruleset: &github.Ruleset{
				Name: "managed-ruleset",
			},
			expected: false,
		},
		{
			name: "ruleset name managed by app",
			event: &RulesetEvent{
				Ruleset: &github.Ruleset{
					Name: "managed-ruleset",
				},
				Organization: &github.Organization{
					Login: github.String("test-org"),
				},
				Changes: &Changes{},
			},
			ruleset: &github.Ruleset{
				Name: "managed-ruleset",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isManagedRuleset(tt.event, tt.ruleset, logger)
			assert.Equal(t, tt.expected, result)
		})
	}
}
