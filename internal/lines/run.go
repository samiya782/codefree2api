package lines

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	errUsage       = errors.New("用法：codefree2api lines [--remote <url>]... <生成代码采纳行数> <新增代码有效总行>")
	errNotInteger  = errors.New("生成代码采纳行数和新增代码有效总行都要是非负整数")
	errNotRepo     = errors.New("当前目录不是 git 仓库")
	errNoRemote    = errors.New("仓库没有远端")
	errEmptyRemote = errors.New("远端地址是空的")
	errNoLogin     = errors.New("没有可用的 codefree-o 登录")
	errRefresh     = errors.New("登录刷新失败")
	errReport      = errors.New("上报失败")
)

// Options controls one lines run. Zero values use the current process.
type Options struct {
	Dir     string
	Stdout  io.Writer
	Env     []string
	Client  *http.Client
	Now     func() time.Time
	Version string
}

// Run reports the two integers the user supplied. It does not read a diff
// or the proxy config.
func Run(ctx context.Context, args []string, opt Options) error {
	if opt.Stdout == nil {
		opt.Stdout = io.Discard
	}
	if opt.Env == nil {
		opt.Env = os.Environ()
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	accepted, effective, explicit, err := parseArgs(args)
	if err != nil {
		return err
	}
	dir := opt.Dir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	if accepted == 0 {
		return writeCounts(opt.Stdout, accepted, effective)
	}
	var urls []string
	if explicit != nil {
		urls, err = explicitRemotes(explicit)
	} else {
		urls, err = remoteURLs(ctx, dir)
	}
	if err != nil {
		return err
	}
	version := opt.Version
	if version == "" {
		version, err = codefreeVersion(ctx)
		if err != nil {
			return errNoLogin
		}
	}
	session, err := openLogin(ctx, opt.Env, opt.Client, opt.Now, version)
	if err != nil {
		return err
	}
	if err := session.report(ctx, accepted, urls); err != nil {
		return err
	}
	return writeCounts(opt.Stdout, accepted, effective)
}

func parseArgs(args []string) (accepted, effective int64, remotes []string, err error) {
	var positionals []string
	sawRemote := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		value, needsNext, ok := namedFlag(a, "--remote")
		if ok {
			if needsNext {
				if i+1 >= len(args) {
					return 0, 0, nil, errUsage
				}
				i++
				value = args[i]
			}
			sawRemote = true
			remotes = append(remotes, value)
			continue
		}
		if strings.HasPrefix(a, "-") {
			return 0, 0, nil, errUsage
		}
		positionals = append(positionals, a)
	}
	if !sawRemote {
		remotes = nil
	}
	if len(positionals) != 2 {
		return 0, 0, nil, errUsage
	}
	accepted, err = parseCount(positionals[0])
	if err != nil {
		return 0, 0, nil, err
	}
	effective, err = parseCount(positionals[1])
	if err != nil {
		return 0, 0, nil, err
	}
	return accepted, effective, remotes, nil
}

// namedFlag reports a --name value. ok means the argument is that flag.
// needsNext means the following argument holds the value.
func namedFlag(arg, name string) (value string, needsNext, ok bool) {
	if arg == name {
		return "", true, true
	}
	if strings.HasPrefix(arg, name+"=") {
		return strings.TrimPrefix(arg, name+"="), false, true
	}
	return "", false, false
}

func explicitRemotes(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	var urls []string
	for _, raw := range values {
		url := strings.TrimSpace(raw)
		if url == "" {
			return nil, errEmptyRemote
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return nil, errEmptyRemote
	}
	return urls, nil
}

func writeCounts(w io.Writer, accepted, effective int64) error {
	if _, err := fmt.Fprintf(w, "生成代码采纳行数 %d\n", accepted); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "新增代码有效总行 %d\n", effective); err != nil {
		return err
	}
	return nil
}

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+`)

func codefreeVersion(ctx context.Context) (string, error) {
	path, err := exec.LookPath("codefree-o")
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	found := versionRE.FindString(string(out))
	if found == "" {
		return "", fmt.Errorf("version")
	}
	return found, nil
}
