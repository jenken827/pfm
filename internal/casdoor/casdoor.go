package casdoor

import (
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// Init 初始化casdoor
func Init() {
	cd := config.GetConfig().Casdoor

	casdoorsdk.InitConfig(cd.Endpoint, cd.ClientID, cd.ClientSecret, cd.Certificate, cd.OrganizationName, cd.ApplicationName)

	llog.L().Info("casdoor init success")
}
