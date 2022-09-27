package svc

import (
	"fmt"
	"github.com/SunMaybo/zero/common/zlog"
	"github.com/SunMaybo/zgrpc/config"
	"github.com/SunMaybo/zgrpc/grpcurl"
	"github.com/boltdb/bolt"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	"log"
	"os"
	"os/user"
)

type ServiceContext struct {
	Cfg         *config.Config
	GrpcSession *grpcurl.GrpcSession
	DB          *bolt.DB
	TaskDB      *gorm.DB
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
	taskDb, err := gorm.Open("mysql", "task_rw:s77NqQBcJghus55p@(10.135.41.205:3336)/task?charset=utf8&parseTime=True&loc=Local")
	if err != nil {
		fmt.Errorf("创建数据库连接失败:%v", err)

	}
	return &ServiceContext{
		DB:          db,
		Cfg:         cfg,
		GrpcSession: grpcurl.GrpcConnection(cfg.Target),
		TaskDB:      taskDb,
	}
}
