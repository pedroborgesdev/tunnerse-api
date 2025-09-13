package main

import (
	"fmt"
	"os"
	"tunnerse/config"
	"tunnerse/debug"
	"tunnerse/expose"
	"tunnerse/logger"
	"tunnerse/middlewares"
	"tunnerse/routes"

	"github.com/gin-gonic/gin"
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
