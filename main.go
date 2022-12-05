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
	flag.StringVar(&watchTarget, "f", "", "watch file")
}

func main() {
	flag.Parse()
	filename := watchTarget
	command := flag.Args()

	fmt.Fprintf(os.Stdout, "exec: %s\n", strings.Join(command, " "))
	events := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())

	st, err := os.Stat(filename)
	if err != nil {
		panic(err)
	}
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
	runCommand(ctx, command, events)
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
		// validate
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
			fmt.Println("canceled")
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

func runCommand(ctx context.Context, command []string, e chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			cmdProcess.KillAll()
			fmt.Println("\ncanceled")
			return
		case <-e:
			cmdProcess.KillAll()
			cmd := exec.Command(command[0], command[1:]...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Start()
			fmt.Println("start process: ", cmd.Process.Pid)
			cmdProcess.SetProcess(cmd.Process)
			if err != nil {
				panic(err)
			}
		}

	}
}

var cmdProcess = &Process{}

type Process struct {
	process []*os.Process
	sync.RWMutex
}

func (p *Process) SetProcess(process *os.Process) {
	p.Lock()
	defer p.Unlock()
	p.process = append(p.process, process)
}

func (p *Process) KillAll() {
	p.RLock()
	defer p.RUnlock()
	pp := make([]*os.Process, 0, len(p.process))
	copy(pp, p.process)
	for i, v := range pp {
		fmt.Printf("kill process: %d", v.Pid)
		if err := v.Kill(); err != nil {
			panic(err)
		}
		fmt.Println(" - ok")
		p.process = slices.Delete(p.process, i, i+1)
	}
}
