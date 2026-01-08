package main

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/config"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/debug"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/expose"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/logger"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/middlewares"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/routes"
)

func main() {
	go func() {
		err := expose.Expose()
		if err != nil {
			fmt.Printf("\nFailed to expose: %s\n", err.Error())
			os.Exit(0)
		}
	}()

	debug.LoadDebugConfig()

	logger.Log("INFO", "Application has been started", []logger.LogDetail{})

	config.LoadAppConfig()

	// database.InitDB()

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	router.Use(
		middlewares.CORSMiddleware(),
	)

	routes.SetupRoutes(router)

	router.Run(":" + config.AppConfig.HTTPPort)

	select {}
}
