package main

import (
	"flag"
	"zgrpc/apis/apis/zgrpc/config"
	"zgrpc/apis/apis/zgrpc/server"
	"zgrpc/apis/apis/zgrpc/svc"
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
