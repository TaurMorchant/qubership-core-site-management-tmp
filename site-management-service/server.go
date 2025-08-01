package main

import (
	fiberSec "github.com/netcracker/qubership-core-lib-go-fiber-server-utils/v2/security"
	"github.com/netcracker/qubership-core-lib-go/v3/security"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-lib-go/v3/utils"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/idp"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/lib"
)

func init() {
	serviceloader.Register(1, &idp.DummyRetryableClient{})
	serviceloader.Register(1, &security.DummyToken{})
	serviceloader.Register(1, utils.NewResourceGroupAnnotationsMapper("qubership.cloud"))
	serviceloader.Register(1, &fiberSec.DummyFiberServerSecurityMiddleware{})
}

//go:generate go run github.com/swaggo/swag/cmd/swag init --generalInfo /controller/api.go --parseDependency --parseGoList=false --parseDepth 2
func main() {
	lib.RunService()
}
