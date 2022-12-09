package main

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

var (
	watchTarget   string
	watchInterval int
	tick          int
)

const defaultTarget = "./"

func init() {
	log.SetFlags(log.Lshortfile)
	flag.StringVar(&watchTarget, "w", "", "watch target. if not specified, the current directory is the target.")
	flag.IntVar(&watchInterval, "i", 100, "watch interval (ms)")
	flag.IntVar(&tick, "t", 0, "run every t ms")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stdout, "watch error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	command := flag.Args()
	wt := watchTarget

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
		wt = defaultTarget
	}

	if wt != "" {
		st, err := os.Stat(wt)
		if err != nil {
			return err
		}

		if st.IsDir() {
			walk(ctx, wt, events)
		} else {
			go watch(ctx, wt, events)
		}

		go func() {
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
			<-sig
			cancel()
		}()
	}

	// 初回プロセス用
	events <- struct{}{}
	cmd := NewCommand(command)
	return cmd.Run(ctx, events)
}

func walk(ctx context.Context, target string, e chan struct{}) {
	if slices.Contains([]string{".git", ".github", "node_modules"}, target) {
		fmt.Fprintf(os.Stdout, "skip: %s\n", target)
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
				// 	fmt.Fprintf(os.Stdout, "skip: %s\n", v.Name())
				// 	return
				// }
				go watch(ctx, path.Join(target, v.Name()), e)
			}
		}
	}
}

func watch(ctx context.Context, filename string, e chan struct{}) {
	fmt.Fprintf(os.Stdout, "watch: %s\n", filename)
	info, _ := os.Stat(filename)
	lastModified := info.ModTime()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stdout, "canxel\n")
			return
		default:
			info, _ := os.Stat(filename)

			if info.ModTime() != lastModified {
				lastModified = info.ModTime()
				e <- struct{}{}
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
	fmt.Fprintf(os.Stdout, "exec: %s\n", strings.Join(cmd, " "))
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

func (c *Command) Start(ctx context.Context) error {
	c.KillAll()
	cmd := exec.CommandContext(ctx, "bash", "-c", c.cmdString)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	c.Lock()
	defer c.Unlock()
	fmt.Fprintf(os.Stdout, "start process: %d\n", cmd.Process.Pid)
	c.cmdProcess = append(c.cmdProcess, cmd)
	return nil
}

func (p *Command) KillAll() {
	p.Lock()
	defer p.Unlock()
	pp := make([]*exec.Cmd, len(p.cmdProcess))
	copy(pp, p.cmdProcess)
	for i, v := range pp {
		fmt.Fprintf(os.Stdout, "terminate process: %d", v.Process.Pid)
		if err := syscall.Kill(-v.Process.Pid, syscall.SIGTERM); err != nil && err != syscall.EPERM {
			panic(err)
		}
		fmt.Fprintf(os.Stdout, " - ok\n")
		_, _ = v.Process.Wait()
		p.cmdProcess = slices.Delete(p.cmdProcess, i, i+1)
	}
}
