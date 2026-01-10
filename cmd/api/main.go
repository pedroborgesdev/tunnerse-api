package main

import (
	"github.com/gin-gonic/gin"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/config"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/debug"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/logger"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/middlewares"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/routes"
)

func main() {
	_ = debug.LoadDebugConfig()
	config.LoadAppConfig()

	// errCh, err := expose.StartExpose()
	// if err != nil {
	// 	fmt.Printf("\nFailed to start expose: %s\n", err.Error())
	// 	os.Exit(1)
	// }
	// go func() {
	// 	if exposeErr := <-errCh; exposeErr != nil {
	// 		fmt.Printf("\nExpose error: %s\n", exposeErr.Error())
	// 		os.Exit(1)
	// 	}
	// }()

	logger.Log("INFO", "Application has been started", []logger.LogDetail{})

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	router.Use(
		middlewares.CORSMiddleware(),
	)

	routes.SetupRoutes(router)

	router.Run(":" + config.AppConfig.HTTPPort)
}
