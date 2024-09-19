package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/kuhlman-labs/repo-ruleset-bot/reporulesetbot"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/rcrowley/go-metrics"
	"github.com/rs/zerolog"
)

func main() {
	config, err := reporulesetbot.ReadConfig("config.yml")
	if err != nil {
		panic(err)
	}

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &logger

	metricsRegistry := metrics.DefaultRegistry

	cc, err := githubapp.NewDefaultCachingClientCreator(
		config.Github,
		githubapp.WithClientUserAgent("repo-ruleset-bot/1.0.0"),
		githubapp.WithClientTimeout(3*time.Second),
		githubapp.WithClientCaching(false, func() httpcache.Cache { return httpcache.NewMemoryCache() }),
		githubapp.WithClientMiddleware(
			githubapp.ClientMetrics(metricsRegistry),
		),
	)
	if err != nil {
		panic(err)
	}

	repoRulesetHandler := reporulesetbot.RulesetHandler{
		ClientCreator: cc,
		Logger:        logger,
	}

	webhookHandler := githubapp.NewDefaultEventDispatcher(config.Github, &repoRulesetHandler)

	http.Handle(githubapp.DefaultWebhookRoute, webhookHandler)

	addr := fmt.Sprintf("%s:%d", config.Server.Address, config.Server.Port)
	logger.Info().Msgf("Starting server on %s...", addr)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server.")
	}
}
