package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/clash_link"
	"gorm.io/gorm"
)

// RegisterClashLinkRoutes registers clash link routes on the gin engine.
// This is called from web.go after API routes are set up.
func RegisterClashLinkRoutes(apiGroup, publicGroup *gin.RouterGroup, db *gorm.DB, storeDir string) error {
	ctrl, err := clash_link.NewController(db, storeDir)
	if err != nil {
		return err
	}
	ctrl.RegisterRoutes(apiGroup, publicGroup)
	return nil
}
