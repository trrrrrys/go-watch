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
	watchTarget string
)

func init() {
	log.SetFlags(log.Lshortfile)
	flag.StringVar(&watchTarget, "f", "./", "watch target")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stdout, "watch error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	filename := watchTarget
	command := flag.Args()

	fmt.Fprintf(os.Stdout, "exec: %s\n", strings.Join(command, " "))
	events := make(chan struct{}, 1)

	st, err := os.Stat(filename)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	if st.IsDir() {
		walk(ctx, filename, events)
	} else {
		go watch(ctx, filename, events)
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		cancel()
	}()

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

			time.Sleep(100 * time.Millisecond)
		}
	}
}

type Command struct {
	cmd     []string
	process []*os.Process
	sync.Mutex
}

func NewCommand(cmd []string) *Command {
	return &Command{
		cmd: cmd,
	}
}

func (c *Command) Run(ctx context.Context, e chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			c.KillAll()
			return fmt.Errorf("canceled")
		case <-e:
			c.KillAll()
			cmd := exec.CommandContext(ctx, c.cmd[0], c.cmd[1:]...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Start()
			c.SetProcess(cmd.Process)
			if err != nil {
				return err
			}
		}

	}
}

func (c *Command) SetProcess(p *os.Process) {
	c.Lock()
	defer c.Unlock()
	fmt.Fprintf(os.Stdout, "start process: %d\n", p.Pid)
	c.process = append(c.process, p)
}

func (p *Command) KillAll() {
	p.Lock()
	defer p.Unlock()
	pp := make([]*os.Process, len(p.process))
	copy(pp, p.process)
	for i, v := range pp {
		fmt.Fprintf(os.Stdout, "terminate process: %d", v.Pid)
		if err := syscall.Kill(-v.Pid, syscall.SIGTERM); err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stdout, " - ok\n")
		p.process = slices.Delete(p.process, i, i+1)
	}
}
