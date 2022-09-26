package svc

import (
	"github.com/SunMaybo/zero/common/zlog"
	"github.com/SunMaybo/zgrpc/config"
	"github.com/SunMaybo/zgrpc/grpcurl"
	"github.com/boltdb/bolt"
	"log"
	"os"
	"os/user"
)

type ServiceContext struct {
	Cfg         *config.Config
	GrpcSession *grpcurl.GrpcSession
	DB          *bolt.DB
}

func NewServiceContext(cfg *config.Config) *ServiceContext {
	currentUser, err := user.Current()
	if err != nil {
		log.Fatalf(err.Error())
	}

	dir := currentUser.HomeDir

	err = os.MkdirAll(dir+"/path/to/", os.ModePerm)
	if err != nil {
		zlog.S.Fatal(err)
	}
	db, err := bolt.Open(dir+"/path/to/zgrpc.db", 0600, nil)
	if err != nil {
		zlog.S.Fatal(err)
	}
	return &ServiceContext{
		DB:          db,
		Cfg:         cfg,
		GrpcSession: grpcurl.GrpcConnection(cfg.Target),
	}
}
