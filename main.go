package main

import (
	"flag"
	"github.com/SunMaybo/zgrpc/config"
	"github.com/SunMaybo/zgrpc/server"
	"github.com/SunMaybo/zgrpc/svc"
)

// 设置路由信息
var target string

func init() {
	flag.StringVar(&target, "target", "10.135.61.97:6836", "grpc target address:port")
	flag.Parse()
}
func main() {
	cfg := config.Config{}
	cfg.Target = target
	cfg.Zero.Server.Port = 3000
	cfg.Zero.Server.Timeout = 30 * 60
	svcCtx := svc.NewServiceContext(&cfg)
	server.NewServer(svcCtx).Start()
}
