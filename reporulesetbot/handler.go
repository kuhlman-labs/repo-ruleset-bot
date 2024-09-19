package reporulesetbot

import (
	"context"
	"encoding/json"

	"github.com/google/go-github/v63/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// RulesetHandler handles ruleset events.
type RulesetHandler struct {
	githubapp.ClientCreator
	zerolog.Logger
	RuleSet string
}

// Constants for action and event types
const (
	ActionCreated              = "created"
	ActionEdited               = "edited"
	ActionDeleted              = "deleted"
	ActionReleased             = "released"
	EventTypeRepositoryRuleset = "repository_ruleset"
	EventTypeInstallation      = "installation"
	EventTypeRelease           = "release"
)

// RulesetEvent represents a GitHub ruleset event.
type RulesetEvent struct {
	Enterprise   *github.Enterprise   `json:"enterprise,omitempty"`
	Organization *github.Organization `json:"organization,omitempty"`
	Repository   *github.Repository   `json:"repository,omitempty"`
	Action       string               `json:"action,omitempty"`
	Installation *github.Installation `json:"installation,omitempty"`
	Sender       *github.User         `json:"sender,omitempty"`
	Ruleset      *github.Ruleset      `json:"repository_ruleset,omitempty"`
	Changes      *Changes             `json:"changes,omitempty"`
}

// Handles returns the list of event types handled by the RulesetHandler.
func (h *RulesetHandler) Handles() []string {
	return []string{"repository_ruleset", "installation", "release"}
}

// Handle processes the event payload based on the event type.
func (h *RulesetHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {

	logger := h.Logger

	switch eventType {
	case EventTypeRepositoryRuleset:
		return h.handleRepositoryRulesetEvent(ctx, payload, logger)
	case EventTypeInstallation:
		return h.handleInstallationEvent(ctx, payload, logger)
	case EventTypeRelease:
		return h.handleReleaseEvent(ctx, payload, logger)
	default:
		logger.Warn().Msgf("Unhandled event type: %s.", eventType)
		return nil
	}
}

// handleRepositoryRulesetEvent handles repository ruleset events.
func (h *RulesetHandler) handleRepositoryRulesetEvent(ctx context.Context, payload []byte, logger zerolog.Logger) error {
	var event *RulesetEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		logger.Error().Err(err).Msg("Failed to parse repository ruleset event payload.")
		return errors.Wrap(err, "Failed to parse repository ruleset event payload")
	}

	logger.Info().Msgf("Repository ruleset event received for the organization %s: %s.", event.Organization.GetLogin(), event.Action)
	return h.handleRepositoryRuleset(ctx, event, logger)
}

// handleInstallationEvent handles installation events.
func (h *RulesetHandler) handleInstallationEvent(ctx context.Context, payload []byte, logger zerolog.Logger) error {
	var event *github.InstallationEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		logger.Error().Err(err).Msg("Failed to parse installation event payload.")
		return errors.Wrap(err, "Failed to parse installation event payload")
	}

	logger.Info().Msgf("Installation event received for the organization %s: %s.", event.Installation.Account.GetLogin(), event.GetAction())
	return h.handleInstallation(ctx, event, logger)
}

// handleReleaseEvent handles release events.
func (h *RulesetHandler) handleReleaseEvent(ctx context.Context, payload []byte, logger zerolog.Logger) error {
	var event *github.ReleaseEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		logger.Error().Err(err).Msg("Failed to parse release event payload.")
		return errors.Wrap(err, "Failed to parse release event payload")
	}

	logger.Info().Msgf("Release event received for the repository %s: %s.", event.GetRepo().GetFullName(), event.GetAction())
	return h.handleRelease(ctx, event, logger)
}

// handleRepositoryRuleset processes organization ruleset events.
func (h *RulesetHandler) handleRepositoryRuleset(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	switch event.Action {
	case ActionCreated:
		return h.handleRulesetCreated(event, logger)
	case ActionEdited:
		return h.handleRulesetEdited(ctx, event, logger)
	case ActionDeleted:
		return h.handleRulesetDeleted(ctx, event, logger)
	default:
		logger.Warn().Msgf("Unhandled action type: %s.", event.Action)
		return nil
	}
}

// handleRulesetCreated handles the "created" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetCreated(event *RulesetEvent, logger zerolog.Logger) error {
	logger.Info().Msgf("Ruleset %s has been created in the organization %s by %s.", event.Ruleset.Name, event.Organization.GetLogin(), event.Sender.GetLogin())
	return nil
}

// handleRulesetEditedhandles the "edited" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetEdited(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	orgName := event.Organization.GetLogin()
	eventInstallationID := event.Installation.GetID()
	eventSender := event.Sender.GetLogin()
	rulesetID := event.Ruleset.GetID()
	eventRulesetName := event.Ruleset.Name

	jwtclient, err := newJWTClient()
	if err != nil {
		return errors.Wrap(err, "Failed to create JWT client")
	}

	app, err := getAuthenticatedApp(ctx, jwtclient)
	if err != nil {
		return errors.Wrap(err, "Failed to get authenticated app")
	}

	appName := app.GetSlug() + "[bot]"

	client, err := h.ClientCreator.NewInstallationClient(eventInstallationID)
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	if eventSender == appName {
		logger.Info().Msgf("Ruleset %s in the organization %s was edited by app %s.", eventRulesetName, orgName, appName)
		return nil
	} else {
		logger.Info().Msgf("Ruleset %s in the organization %s was edited by the user %s.", eventRulesetName, orgName, eventSender)

		rulesets, err := h.readMultipleRulesets(ctx, client, orgName, logger)
		if err != nil {
			return errors.Wrap(err, "Failed to read rulesets from file")
		}

		for _, ruleset := range rulesets {
			if ruleset.Name != eventRulesetName {
				continue
			}
			if !isManagedRuleset(event, ruleset, logger) {
				return nil
			}
			if err := editRuleset(ctx, client, orgName, rulesetID, ruleset, logger); err != nil {
				return err
			}
		}
		return nil
	}
}

// handleRulesetDeleted handles the "deleted" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetDeleted(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	eventRulesetName := event.Ruleset.Name
	orgName := event.Organization.GetLogin()
	logger.Info().Msgf("Ruleset %s has been deleted in the organization %s by %s.", eventRulesetName, orgName, event.Sender.GetLogin())

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	rulesets, err := h.readMultipleRulesets(ctx, client, orgName, logger)
	if err != nil {
		return errors.Wrap(err, "Failed to read rulesets from file")
	}

	for _, ruleset := range rulesets {
		if ruleset.Name == eventRulesetName {

			rulesetName := ruleset.Name

			if !isManagedRuleset(event, ruleset, logger) {
				return nil
			}

			logger.Info().Msgf("Recreating ruleset %s in organization %s.", rulesetName, orgName)

			if err := createRuleset(ctx, client, orgName, ruleset, logger); err != nil {
				return err
			}
			break
		}
	}

	return nil
}

// handleInstallation processes installation events.
func (h *RulesetHandler) handleInstallation(ctx context.Context, event *github.InstallationEvent, logger zerolog.Logger) error {
	installationID := event.GetInstallation().GetID()
	orgName := event.Installation.Account.GetLogin()
	action := event.GetAction()
	appName := event.GetInstallation().GetAppSlug()

	logger.Info().Msgf("Application %s was installed in the organization %s.", appName, orgName)

	if action != ActionCreated {
		return nil
	}

	client, err := h.ClientCreator.NewInstallationClient(installationID)
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	rulesets, err := h.readMultipleRulesets(ctx, client, orgName, logger)
	if err != nil {
		return errors.Wrap(err, "Failed to read rulesets from file")
	}

	logger.Info().Msgf("Found %d rulesets configured.", len(rulesets))

	for _, ruleset := range rulesets {
		logger.Info().Msgf("Creating ruleset %s in organization %s.", ruleset.Name, orgName)
		if err := createRuleset(ctx, client, orgName, ruleset, logger); err != nil {
			return err
		}
	}

	return nil
}

// handleRelease processes release events.
func (h *RulesetHandler) handleRelease(ctx context.Context, event *github.ReleaseEvent, logger zerolog.Logger) error {
	repoName := event.GetRepo().GetFullName()
	action := event.GetAction()
	tagName := event.GetRelease().GetTagName()
	actionType := event.GetAction()

	if actionType != ActionReleased {
		return nil
	}

	jwtclient, err := newJWTClient()
	if err != nil {
		return errors.Wrap(err, "Failed to create JWT client")
	}

	app, err := getAuthenticatedApp(ctx, jwtclient)
	if err != nil {
		return errors.Wrap(err, "Failed to get app")
	}

	appRepoURL := app.GetExternalURL()

	appRepoName, err := getRepoFullNameFromURL(appRepoURL)
	if err != nil {
		return errors.Wrap(err, "Failed to get app repo name")
	}

	if repoName != appRepoName {
		return nil
	}

	logger.Info().Msgf("Release %s was %s for the repository %s.", tagName, action, repoName)
	logger.Info().Msgf("Updating the rulesets...")

	//get installations for the app
	installations, err := getOrgInstallations(ctx, jwtclient)
	if err != nil {
		return errors.Wrap(err, "Failed to get installations for authenticated app")
	}

	for orgName, installation := range installations {
		client, err := h.ClientCreator.NewInstallationClient(installation)
		if err != nil {
			return errors.Wrap(err, "Failed to create installation client")
		}

		rulesets, err := h.readMultipleRulesets(ctx, client, orgName, logger)
		if err != nil {
			return errors.Wrap(err, "Failed to read rulesets from file")
		}

		for _, ruleset := range rulesets {

			rulesetName := ruleset.Name

			rulesetID, err := getOrgRulesets(ctx, client, orgName, rulesetName)
			if err != nil {
				return errors.Wrap(err, "Failed to get ruleset ID")
			}

			if err := editRuleset(ctx, client, orgName, rulesetID, ruleset, logger); err != nil {
				return err
			}
		}
	}

	return nil
}
