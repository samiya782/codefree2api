package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"codefree2api/internal/proxy"
)

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) > 1 && os.Args[1] == "launch" {
		if err := runLaunch(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	opt, err := optionsFromFlags(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if err := serve(opt); err != nil {
		log.Fatal(err)
	}
}

func optionsFromFlags(args []string) (proxy.Options, error) {
	fs := flag.NewFlagSet("codefree2api", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "配置文件路径")
	port := fs.Int("port", proxy.DefaultPort, "监听端口（仅 127.0.0.1）")
	password := fs.String("password", "", "可选 Bearer 口令；未设置则本机免鉴权")
	bin := fs.String("bin", "", "覆盖 codefree-o 可执行文件路径")
	if err := fs.Parse(args); err != nil {
		return proxy.Options{}, err
	}
	cfg, err := proxy.LoadConfig(*configPath)
	if err != nil {
		return proxy.Options{}, err
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
	return proxy.Options{Port: *port, Password: *password, Bin: *bin, Aliases: cfg.ModelAliases}, nil
}

func serve(opt proxy.Options) error {
	srv, err := proxy.New(opt)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", srv.Addr())
	if err != nil {
		return fmt.Errorf("listen %s: %w", srv.Addr(), err)
	}
	hs := srv.HTTPServer()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	log.Printf("codefree2api listening on http://%s bin=%s", srv.Addr(), srv.Bin())
	err = hs.Serve(ln)
	_ = srv.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runLaunch(args []string) error {
	tool, model, extra, opt, err := parseLaunch(args)
	if err != nil {
		return err
	}
	if opt.Port == 0 {
		opt.Port = proxy.DefaultPort
	}
	srv, err := proxy.New(opt)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", srv.Addr())
	if err != nil {
		return fmt.Errorf("listen %s: %w", srv.Addr(), err)
	}
	hs := srv.HTTPServer()
	defer func() { _ = srv.Close() }()
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("proxy: %v", err)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + srv.Addr() + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	spec, err := proxy.LaunchCommand(tool, opt.Port, opt.Password, model, extra)
	if err != nil {
		return err
	}
	path, err := exec.LookPath(spec.Name)
	if err != nil {
		return fmt.Errorf("%s not found on PATH", spec.Name)
	}
	cmd := exec.Command(path, spec.Args...)
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("launch %s via http://%s model=%s", spec.Name, srv.Addr(), model)
	return cmd.Run()
}

func parseLaunch(args []string) (tool, model string, extra []string, opt proxy.Options, err error) {
	configPath := "config.json"
	var port int
	var password, bin string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			extra = append(extra, args[i+1:]...)
			break
		}
		need := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		switch a {
		case "--model", "-model", "-m":
			model, err = need()
		case "--port", "-port":
			var v string
			v, err = need()
			if err == nil {
				port, err = strconv.Atoi(v)
			}
		case "--password", "-password":
			password, err = need()
		case "--bin", "-bin":
			bin, err = need()
		case "--config", "-config":
			configPath, err = need()
		default:
			if strings.HasPrefix(a, "-") {
				return "", "", nil, proxy.Options{}, fmt.Errorf("unknown flag %s", a)
			}
			if tool == "" {
				tool = a
			} else {
				extra = append(extra, a)
			}
		}
		if err != nil {
			return "", "", nil, proxy.Options{}, err
		}
	}
	if tool == "" {
		return "", "", nil, proxy.Options{}, fmt.Errorf("usage: codefree2api launch claude|codex [--model name] [-- extra args]")
	}
	cfg, err := proxy.LoadConfig(configPath)
	if err != nil {
		return "", "", nil, proxy.Options{}, err
	}
	if port == 0 {
		port = cfg.Port
	}
	if password == "" {
		password = cfg.Password
	}
	if bin == "" {
		bin = cfg.Bin
	}
	opt = proxy.Options{Port: port, Password: password, Bin: bin, Aliases: cfg.ModelAliases}
	return tool, model, extra, opt, nil
}
