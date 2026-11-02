package main

import (
	"flag"
	"log"
	"net"
	"os"

	"codefree2api/internal/proxy"
)

func main() {
	log.SetFlags(log.LstdFlags)
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	configPath := fs.String("config", "config.json", "配置文件路径")
	port := fs.Int("port", proxy.DefaultPort, "监听端口（仅 127.0.0.1）")
	password := fs.String("password", "", "可选 Bearer 口令；未设置则本机免鉴权")
	bin := fs.String("bin", "", "覆盖 codefree-o 可执行文件路径")
	_ = fs.Parse(os.Args[1:])

	cfg, err := proxy.LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if !set["port"] && cfg.Port != 0 {
		*port = cfg.Port
	}
	if !set["password"] && cfg.Password != "" {
		*password = cfg.Password
	}
	if !set["bin"] && cfg.Bin != "" {
		*bin = cfg.Bin
	}
	if *bin == "" {
		*bin = proxy.DefaultBin
	}

	srv, err := proxy.New(proxy.Options{
		Port:     *port,
		Password: *password,
		Bin:      *bin,
	})
	if err != nil {
		log.Fatal(err)
	}

	httpSrv := srv.HTTPServer()
	ln, err := net.Listen("tcp", srv.Addr())
	if err != nil {
		log.Fatalf("listen %s: %v", srv.Addr(), err)
	}
	log.Printf("codefree2api listening on http://%s bin=%s", srv.Addr(), srv.Bin())
	if err := httpSrv.Serve(ln); err != nil {
		log.Fatal(err)
	}
}
