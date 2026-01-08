package routes

import (
	"net/http"

	"github.com/pedroborgesdev/tunnerse-api/internal/api/config"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/controllers"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(router *gin.Engine) {

	tunnelController := controllers.NewTunnelController()

	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	tunnel := router.Group("/")

	if config.AppConfig.SUBDOMAIN {
		tunnel.POST("/register", tunnelController.Register)
		tunnel.GET("/tunnel", tunnelController.Get)
		tunnel.POST("/response", tunnelController.Response)
		tunnel.POST("/close", tunnelController.Close)
		tunnel.GET("/", tunnelController.Tunnel)
		tunnel.HEAD("/", tunnelController.Tunnel)

		router.NoRoute(tunnelController.Tunnel)
	}

	if !config.AppConfig.SUBDOMAIN {
		tunnel.POST("/register", tunnelController.Register)
		tunnel.GET(":name/tunnel", tunnelController.Get)
		tunnel.POST(":name/response", tunnelController.Response)
		tunnel.POST(":name/close", tunnelController.Close)
		tunnel.GET(":name/", tunnelController.Tunnel)
		tunnel.HEAD(":name/", tunnelController.Tunnel)

		router.NoRoute(tunnelController.Tunnel)
	}
}
