package main

import (
	"github.com/rs/zerolog"
	"github.com/tr4cks/power/modules"

	"github.com/gin-gonic/gin"
)

func ServerStateMiddleware(module modules.Module, logger *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		powerState, ledState := module.State()

		if powerState.Err != nil {
			logger.Error().Err(powerState.Err).Msg("Failed to retrieve POWER state")
		}
		if ledState.Err != nil {
			logger.Error().Err(ledState.Err).Msg("Failed to retrieve LED state")
		}

		c.Set("power", powerState.Value)
		c.Set("led", ledState.Value)

		c.Next()
	}
}

func ConditionalMiddleware(predicate func(*gin.Context) bool, middleware gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if predicate(c) {
			middleware(c)
		} else {
			c.Next()
		}
	}
}
