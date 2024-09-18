package reporulesetbot

import (
	"context"
	"net/http"
	"os"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v63/github"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

func (h *RulesetHandler) createRuleset(ctx context.Context, client *github.Client, orgName string, ruleset *github.Ruleset, logger zerolog.Logger) error {
	if _, _, err := client.Organizations.CreateOrganizationRuleset(ctx, orgName, ruleset); err != nil {
		return errors.Wrap(err, "Failed to deploy repository ruleset")
	}
	logger.Info().Msgf("Successfully created the %s ruleset for organization %s.", ruleset.Name, orgName)
	return nil
}

func (h *RulesetHandler) editRuleset(ctx context.Context, client *github.Client, orgName string, rulesetID int64, ruleset *github.Ruleset, logger zerolog.Logger) error {
	if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, orgName, rulesetID, ruleset); err != nil {
		return errors.Wrap(err, "Failed to update repository ruleset")
	}
	logger.Info().Msgf("Successfully updated the %s ruleset for organization %s.", ruleset.Name, orgName)
	return nil
}

// getRepoID returns the repository ID from a given repository name.
func getRepoID(ctx context.Context, client *github.Client, owner, repo string) (int64, error) {
	repository, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get repository")
	}

	return repository.GetID(), nil
}

// getRepoName returns the repository name from a given repository ID.
func getRepoName(ctx context.Context, client *github.Client, repoID int64) (string, error) {
	repository, _, err := client.Repositories.GetByID(ctx, repoID)
	if err != nil {
		return "", errors.Wrap(err, "Failed to get repository")
	}

	return repository.GetName(), nil
}

// getTeamByName returns the team by its name.
func getTeamByName(ctx context.Context, client *github.Client, orgName, teamName string) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamBySlug(ctx, orgName, teamName)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get team")
	}
	return team, nil
}

// getCustomRepoRolesForOrg returns the custom repository roles for an organization.
func getCustomRepoRolesForOrg(ctx context.Context, client *github.Client, orgName string) (*github.OrganizationCustomRepoRoles, error) {
	customRepoRoles, _, err := client.Organizations.ListCustomRepoRoles(ctx, orgName)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get custom repository roles for organization: %s", orgName)
	}
	return customRepoRoles, nil
}

// getAuthenticatedApp returns the authenticated app name.
func getAuthenticatedApp(ctx context.Context, client *github.Client) (string, error) {
	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return "", errors.Wrap(err, "Failed to get app")
	}
	return app.GetName(), nil
}

// newJWTClient creates a new JWT client.
func newJWTClient() (*github.Client, error) {
	config, err := ReadConfig("config.yml")
	if err != nil {
		panic(err)
	}

	// Create a file of the private key
	_, err = os.Create("private-key.pem")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create private key file")
	}

	// Write the configured private key to the file
	err = os.WriteFile("private-key.pem", []byte(config.Github.App.PrivateKey), 0600)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to write private key to file")
	}

	// Create a new JWT client
	itr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, config.Github.App.IntegrationID, "private-key.pem")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create JWT client")
	}
	client := github.NewClient(&http.Client{Transport: itr})

	// Delete the private key file
	err = os.Remove("private-key.pem")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to delete private key file")
	}

	return client, nil
}
