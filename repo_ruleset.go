package main

import (
	"context"
	"encoding/json"

	"github.com/google/go-github/v63/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
)

type RulesetEvent struct {
	Enterprise   github.Enterprise   `json:"enterprise,omitempty"`
	Organization github.Organization `json:"organization,omitempty"`
	Repository   github.Repository   `json:"repository,omitempty"`
	Action       string              `json:"action,omitempty"`
	Installation github.Installation `json:"installation,omitempty"`
	Sender       github.User         `json:"sender,omitempty"`
	Ruleset      github.Ruleset      `json:"ruleset,omitempty"`
	Changes      Changes             `json:"changes,omitempty"`
}

type Changes struct {
	Rules struct {
		Added []struct {
			Type       string `json:"type"`
			Parameters struct {
				RequiredApprovingReviewCount     int  `json:"required_approving_review_count,omitempty"`
				DismissStaleReviewsOnPush        bool `json:"dismiss_stale_reviews_on_push,omitempty"`
				RequireCodeOwnerReview           bool `json:"require_code_owner_review,omitempty"`
				RequireLastPushApproval          bool `json:"require_last_push_approval,omitempty"`
				RequiredReviewThreadResolution   bool `json:"required_review_thread_resolution,omitempty"`
				StrictRequiredStatusChecksPolicy bool `json:"strict_required_status_checks_policy,omitempty"`
				DoNotEnforceOnCreate             bool `json:"do_not_enforce_on_create,omitempty"`
				RequiredStatusChecks             []struct {
					Context string `json:"context"`
				} `json:"required_status_checks,omitempty"`
				Workflows []struct {
					RepositoryID int    `json:"repository_id"`
					Path         string `json:"path"`
					Ref          string `json:"ref"`
				} `json:"workflows,omitempty"`
				CodeScanningTools []struct {
					Tool                    string `json:"tool"`
					SecurityAlertsThreshold string `json:"security_alerts_threshold"`
					AlertsThreshold         string `json:"alerts_threshold"`
				} `json:"code_scanning_tools,omitempty"`
				Operator string `json:"operator,omitempty"`
				Pattern  string `json:"pattern,omitempty"`
				Negate   bool   `json:"negate,omitempty"`
				Name     string `json:"name,omitempty"`
			} `json:"parameters,omitempty"`
		} `json:"added"`
		Deleted []interface{} `json:"deleted"`
	} `json:"rules"`
	Conditions struct {
		Added []struct {
			RepositoryProperty struct {
				Exclude []interface{} `json:"exclude"`
				Include []struct {
					Name           string   `json:"name"`
					Source         string   `json:"source"`
					PropertyValues []string `json:"property_values"`
				} `json:"include"`
			} `json:"repository_property"`
		} `json:"added"`
		Updated []struct {
			Condition struct {
				RefName struct {
					Exclude []interface{} `json:"exclude"`
					Include []string      `json:"include"`
				} `json:"ref_name"`
			} `json:"condition"`
			Changes struct {
				Include struct {
					From []string `json:"from"`
				} `json:"include"`
			} `json:"changes"`
		} `json:"updated"`
		Deleted []struct {
			RepositoryName struct {
				Exclude []interface{} `json:"exclude"`
				Include []string      `json:"include"`
			} `json:"repository_name"`
		} `json:"deleted"`
	} `json:"conditions"`
}

type RulesetHandler struct {
	githubapp.ClientCreator
}

func (h *RulesetHandler) Handles() []string {
	return []string{"repository_ruleset"}
}

func (h *RulesetHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event RulesetEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse repository ruleset event payload")
	}

	return nil
}
