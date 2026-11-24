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

	"codefree2api/internal/srd"
)

var (
	errUsage           = errors.New("用法：codefree2api lines [plugin|space [spec]] [--remote <url>]... [--plugin-version <版本>] [--editor-version <版本>] [--space-version <版本>] <生成代码采纳行数> <新增代码有效总行>")
	errNotInteger      = errors.New("生成代码采纳行数和新增代码有效总行都要是非负整数")
	errNotRepo         = errors.New("当前目录不是 git 仓库")
	errNoRemote        = errors.New("仓库没有远端")
	errEmptyRemote     = errors.New("远端地址是空的")
	errNoLogin         = errors.New("没有可用的 codefree-o 登录")
	errRefresh         = errors.New("登录刷新失败")
	errReport          = errors.New("上报失败")
	errNoPluginVersion = errors.New("没有插件版本")
	errNoEditorVersion = errors.New("没有编辑器版本")
)

// Options controls one lines run. Zero values use the current process.
type Options struct {
	Dir     string
	Stdout  io.Writer
	Stderr  io.Writer
	Env     []string
	Client  *http.Client
	Now     func() time.Time
	Version string
	// Session, Open, BaseURL, and Editor are for tests and in-process login.
	Session *srd.Session
	Open    func(string) error
	BaseURL string
	Editor  func(context.Context) (string, error)
}

type lineArgs struct {
	client        string
	accepted      int64
	effective     int64
	remotes       []string
	explicit      bool
	pluginVersion string
	editorVersion string
	spaceVersion  string
	spaceSpec     bool
}

// Run reports the two integers the user supplied. It does not read a diff
// or the proxy config.
func Run(ctx context.Context, args []string, opt Options) error {
	if opt.Stdout == nil {
		opt.Stdout = io.Discard
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	if opt.Env == nil {
		opt.Env = os.Environ()
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	parsed, err := parseArgs(args)
	if err != nil {
		return err
	}
	switch parsed.client {
	case "plugin":
		return runPlugin(ctx, parsed, opt)
	case "space":
		return runSpace(ctx, parsed, opt)
	default:
		return runCodefree(ctx, parsed, opt)
	}
}

func runCodefree(ctx context.Context, parsed lineArgs, opt Options) error {
	if parsed.accepted == 0 {
		return writeCounts(opt.Stdout, parsed.accepted, parsed.effective)
	}
	dir, err := opt.dir()
	if err != nil {
		return err
	}
	var urls []string
	if parsed.explicit {
		urls, err = explicitRemotes(parsed.remotes)
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
	if err := session.report(ctx, parsed.accepted, urls); err != nil {
		return err
	}
	return writeCounts(opt.Stdout, parsed.accepted, parsed.effective)
}

func (opt Options) dir() (string, error) {
	if opt.Dir != "" {
		return opt.Dir, nil
	}
	return os.Getwd()
}

func parseArgs(args []string) (lineArgs, error) {
	var out lineArgs
	var positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		name, value, needs, ok := versionFlag(a)
		if ok {
			if needs {
				if i+1 >= len(args) {
					return lineArgs{}, errUsage
				}
				i++
				value = args[i]
			}
			if value == "" {
				return lineArgs{}, errUsage
			}
			switch name {
			case "--remote":
				out.explicit = true
				out.remotes = append(out.remotes, value)
			case "--plugin-version":
				out.pluginVersion = value
			case "--editor-version":
				out.editorVersion = value
			case "--space-version":
				out.spaceVersion = value
			default:
				return lineArgs{}, errUsage
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			return lineArgs{}, errUsage
		}
		positionals = append(positionals, a)
	}
	if len(positionals) >= 2 && (positionals[0] == "plugin" || positionals[0] == "space") {
		out.client = positionals[0]
		positionals = positionals[1:]
	}
	if out.client == "space" && len(positionals) == 3 && positionals[0] == "spec" {
		out.spaceSpec = true
		positionals = positionals[1:]
	}
	if len(positionals) != 2 {
		return lineArgs{}, errUsage
	}
	if out.client == "" && (out.pluginVersion != "" || out.editorVersion != "" || out.spaceVersion != "") {
		return lineArgs{}, errUsage
	}
	if out.client == "plugin" && (out.spaceVersion != "" || out.spaceSpec) {
		return lineArgs{}, errUsage
	}
	if out.client == "space" && (out.pluginVersion != "" || out.editorVersion != "") {
		return lineArgs{}, errUsage
	}
	var err error
	out.accepted, err = parseCount(positionals[0])
	if err != nil {
		return lineArgs{}, err
	}
	out.effective, err = parseCount(positionals[1])
	if err != nil {
		return lineArgs{}, err
	}
	return out, nil
}

func versionFlag(arg string) (name, value string, needs, ok bool) {
	flags := []string{"--remote", "--plugin-version", "--editor-version", "--space-version"}
	for _, name := range flags {
		if arg == name {
			return name, "", true, true
		}
		if strings.HasPrefix(arg, name+"=") {
			return name, strings.TrimPrefix(arg, name+"="), false, true
		}
	}
	return "", "", false, false
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

func loadRemotes(ctx context.Context, dir string, parsed lineArgs) (urls []string, gitURL string, err error) {
	if parsed.explicit {
		urls, err = explicitRemotes(parsed.remotes)
		if err != nil {
			return nil, "", err
		}
		return urls, urls[0], nil
	}
	urls, err = remoteURLs(ctx, dir)
	if err != nil {
		return nil, "", err
	}
	gitURL = srd.FirstGitURL(dir)
	if gitURL == "" {
		gitURL = urls[0]
	}
	return urls, gitURL, nil
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
