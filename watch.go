package watch

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/exp/slices"
)

type stringFlags struct {
	values        []string
	defaultValues []string
}

func (s *stringFlags) String() string {
	return strings.Join(s.values, ", ")
}

func (s *stringFlags) Set(str string) error {
	if s.values != nil {
		s.values = append(s.values, str)
	} else {
		s.values = []string{str}
	}
	return nil
}

func (s *stringFlags) SetDefault() {
	if s.values == nil || len(s.values) == 0 {
		s.values = s.defaultValues
	}
}

var (
	watches stringFlags = stringFlags{
		defaultValues: []string{"./"},
	}
	excludes stringFlags = stringFlags{
		defaultValues: []string{".git", ".github", "node_module"},
	}
	watchInterval int
	tick          int
	varbose       bool
)

func init() {
	log.SetFlags(log.Lshortfile)
	flag.Var(&watches, "w", "watch target")
	flag.Var(&excludes, "e", "exclude target")
	flag.IntVar(&tick, "t", 0, "run every t s")
	flag.BoolVar(&varbose, "v", false, "output varbose log")
	flag.IntVar(&watchInterval, "i", 100, "watch interval (ms)")
}

func Run() error {
	flag.Parse()
	excludes.SetDefault()
	command := flag.Args()

	events := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if tick != 0 {
		go func() {
			t := time.Tick(time.Duration(tick) * time.Second)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t:
					events <- struct{}{}
				}

			}
		}()
	} else {
		watches.SetDefault()
	}

	if len(watches.values) != 0 {
		for _, v := range watches.values {
			st, err := os.Stat(v)
			if err != nil {
				return err
			}

			if st.IsDir() {
				walk(ctx, v, events)
			} else {
				go watch(ctx, v, events)
			}
		}
		// 初回プロセス用
		events <- struct{}{}
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		cancel()
	}()

	cmd := NewCommand(command)
	return cmd.Run(ctx, events)
}

func walk(ctx context.Context, target string, e chan struct{}) {
	if slices.Contains(excludes.values, target) {
		Info(LogSkip, target)
		return
	}
	files, err := os.ReadDir(target)
	if err != nil {
		panic(err)
	}
	for _, v := range files {
		if v.Name() != target {
			if v.IsDir() {
				walk(ctx, path.Join(target, v.Name()), e)
			} else {
				// if strings.HasPrefix(v.Name(), ".") {
				// 	varboseLogf("skip: %s\n", v.Name())
				// 	return
				// }
				go watch(ctx, path.Join(target, v.Name()), e)
			}
		}
	}
}

func watch(ctx context.Context, filename string, e chan struct{}) {
	Info(LogWatch, filename)
	// info, _ := os.Stat(filename)
	info, err := os.Stat(filename)
	if err != nil {
		fmt.Fprintf(os.Stdout, "err: %v\n", err)
		return
	}

	lastModified := info.ModTime()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stdout, "canxel\n")
			return
		default:
			info, _ := os.Stat(filename)
			// id := info.Sys().(*syscall.Stat_t).Ino
			// Info(LogWatch, fmt.Sprintf("update from pid: %d", id))

			if info.ModTime() != lastModified {
				lastModified = info.ModTime()
				e <- struct{}{}
				fmt.Fprintf(os.Stdout, "write: %s\n", info.Name())
			}
			time.Sleep(time.Duration(watchInterval) * time.Millisecond)
		}
	}
}

type Command struct {
	cmdString  string
	cmdProcess []*exec.Cmd
	sync.Mutex
}

func NewCommand(cmd []string) *Command {
	Info(LogExec, strings.Join(cmd, " "))
	return &Command{
		cmdString: strings.Join(cmd, " "),
	}
}

func (c *Command) Run(ctx context.Context, e chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			c.KillAll()
			return fmt.Errorf("canceled")
		case <-e:
			if err := c.Start(ctx); err != nil {
				return err
			}
		}

	}
}

type CustomError struct {
	stderr *os.File
}

func (e CustomError) Write(b []byte) (n int, err error) {
	// return e.stderr.Write(append([]byte("========\n"), b...))
	// return e.stderr.Write(append(append([]byte("\x1b[31m"), b...), []byte("\x1b[0m")...))
	return e.stderr.Write(b)
}

func (c *Command) Start(ctx context.Context) error {
	c.KillAll()
	cmd := exec.CommandContext(ctx, "bash", "-c", c.cmdString)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr
	cmd.Stderr = CustomError{os.Stderr}
	// cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	c.Lock()
	defer c.Unlock()
	Info(LogStart, "pid = %d\n", cmd.Process.Pid)
	c.cmdProcess = append(c.cmdProcess, cmd)
	return nil
}

func (p *Command) KillAll() {
	p.Lock()
	defer p.Unlock()
	pp := make([]*exec.Cmd, len(p.cmdProcess))
	copy(pp, p.cmdProcess)
	for i, v := range pp {
		Info(LogTerm, "PID = %d", v.Process.Pid)
		if err := syscall.Kill(-v.Process.Pid, syscall.SIGTERM); err != nil && err != syscall.EPERM {
			panic(err)
		}
		Info(LogTerm, "PID = %d - OK", v.Process.Pid)
		_, _ = v.Process.Wait()
		p.cmdProcess = slices.Delete(p.cmdProcess, i, i+1)
	}
}
