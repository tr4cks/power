package main

import (
	"log"

	"github.com/tr4cks/power/modules"

	"github.com/gin-gonic/gin"
)

func ServerStateMiddleware(module modules.Module, logErr *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		powerState, ledState := module.State()

		if powerState.Err != nil {
			logErr.Printf("Failed to retrieve POWER state: %s", powerState.Err)
		}
		if ledState.Err != nil {
			logErr.Printf("Failed to retrieve LED state: %s", ledState.Err)
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
