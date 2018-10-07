package main

import (
	"bytes"
	"container/ring"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/hpcloud/tail"
	"github.com/mattn/go-shellwords"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const DEFAULT_CONF_FILE = "config.toml"

var (
	version string
	date    string
)

var verbose bool = false

type Config struct {
	FilePath string `toml:"file_path"`
	Rules    []Rule `toml:"rules"`
	Verbose  bool   `toml:"verbose"`
}

type Rule struct {
	Name           string   `toml:name`
	Patterns       []string `toml:"patterns"`
	RegexpPatterns []string `toml:"regexp_patterns"`
	Commands       []string `toml:"commands"`
	Backoff        int      `toml:"backoff"`
	WindowSec      int      `toml:"window_sec"`
	MaxInWindow    int      `toml:"max_in_window"`
}

type CmdArgs []string

type WatchConfig struct {
	Name            string
	Patterns        []string
	CompiledRegexps []*regexp.Regexp
	Commands        [][]string
	Backoff         int
	IgnoreUntil     time.Time
	Channel         chan Event
	Window          time.Duration
	Events          *ring.Ring
}

type Event struct {
	ReadAt time.Time
	Text   string
}

func matchSimple(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func matchRegexp(s string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func (e Event) String() string {
	return fmt.Sprintf("%s %s", e.ReadAt, e.Text)
}

func execCommand(e Event, args []string) {
	log.Printf("INFO exec: %v\n", args)
	cmd_name := args[0]
	opt_args := args[1:]
	cmd := exec.Command(cmd_name, opt_args...)
	cmd.Stdin = strings.NewReader(e.String())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Printf("ERROR %s %v\n", args, err)
	}
	log.Printf("INFO %s STDOUT: %s\n", args, strings.TrimRight(stdout.String(), "\n"))
	log.Printf("INFO %s STDERR: %s\n", args, strings.TrimRight(stderr.String(), "\n"))
}

func overWindowLimit(e Event, conf *WatchConfig) bool {
	conf.Events.Value = e
	conf.Events = conf.Events.Next()
	nextEvent := conf.Events.Value
	if ne, ok := nextEvent.(Event); ok {
		if ne.ReadAt.Unix() > time.Now().Add(-1 * conf.Window).Unix() {
			if verbose {
				log.Println("DEBUG Ring buffer is full")
			}
			return true
		}
	}
	return false
}

func handleMatched(conf *WatchConfig) {
	for {
		event := <-conf.Channel
		if verbose {
			log.Println("DEBUG received:", event.Text)
		}
		if conf.Window.Seconds() > 0 {
			if overWindowLimit(event, conf) != true {
				continue
			}
		}
		for _, args := range conf.Commands {
			go execCommand(event, args)
		}
		if conf.Backoff > 0 {
			conf.IgnoreUntil = time.Now().Add(time.Second * time.Duration(conf.Backoff))
		}
	}
}

func handleInput(event Event, conf *WatchConfig) {
	var match bool
	if len(conf.Patterns) > 0 {
		match = matchSimple(event.Text, conf.Patterns)
	}
	if match == false && len(conf.CompiledRegexps) > 0 {
		match = matchRegexp(event.Text, conf.CompiledRegexps)
	}
	if match == false {
		return
	}
	if conf.Backoff > 0 && event.ReadAt.Unix() < conf.IgnoreUntil.Unix() {
		if verbose {
			log.Printf("DEBUG Skipping matched event: `%s` until: %s\n", event.Text, conf.IgnoreUntil)
		}
		return
	}
	conf.Channel <- event
}

func main() {
	for _, argv := range os.Args {
		if argv == "-v" {
			fmt.Println("gowatch version:", version)
			fmt.Println("build date:", date)
			os.Exit(1)
		} else if argv == "-h" {
			fmt.Println("Usage: gowatch [-v] [-h] [/path/to/config.toml]")
			fmt.Println("       -v: show version")
			fmt.Println("       -h: show this message")
			fmt.Println("use config.toml in the current directory if no config file path is given.")
			os.Exit(1)
		}
	}

	var config Config
	conf_file := DEFAULT_CONF_FILE
	if len(os.Args) > 1 {
		conf_file = os.Args[1]
	}
	_, err := toml.DecodeFile(conf_file, &config)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("INFO tail file:", config.FilePath)
	verbose = config.Verbose

	var watch []*WatchConfig

	// Initialize
	for i, rule := range config.Rules {
		var conf WatchConfig

		// name
		conf.Name = rule.Name
		log.Printf("INFO RuleSet[%d] name: %s\n", i, conf.Name)

		// patterns
		for j, pattern := range rule.RegexpPatterns {
			conf.CompiledRegexps = append(conf.CompiledRegexps, regexp.MustCompile(pattern))
			log.Printf("INFO RuleSet[%d] regexpPattern[%d]: %s\n", i, j, pattern)
		}
		conf.Patterns = rule.Patterns

		// regexp_patterns
		for j, pattern := range rule.Patterns {
			log.Printf("INFO RuleSet[%d] pattern[%d]: %s\n", i, j, pattern)
		}

		// commands
		for j, command := range rule.Commands {
			args, err := shellwords.Parse(command)
			if err != nil {
				log.Fatal(err)
			}
			conf.Commands = append(conf.Commands, args)
			log.Printf("INFO RuleSet[%d] command[%d]: %s\n", i, j, args)
		}
		if len(conf.Commands) == 0 {
			log.Fatalf("no commands defined for rule %s\n", conf.Name)
		}

		if rule.MaxInWindow > 0 && rule.WindowSec > 0 {
			log.Printf("INFO RuleSet[%d] WindowSec: %ds, MaxInWindow: %d\n", i, rule.WindowSec, rule.MaxInWindow)
			conf.Window = time.Second * time.Duration(rule.WindowSec)
			conf.Events = ring.New(rule.MaxInWindow)
		}

		log.Printf("INFO RuleSet[%d] backoff: %d\n", i, rule.Backoff)
		conf.Backoff = rule.Backoff

		conf.Channel = make(chan Event)

		watch = append(watch, &conf)

		go handleMatched(&conf)
	}

	// Open log file
	location := tail.SeekInfo{Offset: 0, Whence: os.SEEK_END}
	t, err := tail.TailFile(config.FilePath,
		tail.Config{
			Location: &location,
			Follow:   true,
			ReOpen:   true,
			Poll:     true})
	if err != nil {
		panic(err)
	}

	// Read log stream
	log.Printf("INFO Starting watch %s\n", config.FilePath)
	for line := range t.Lines {
		if verbose {
			log.Println("DEBUG read:", line.Text)
		}
		// handle each RuleSet
		for _, c := range watch {
			event := Event{ReadAt: time.Now(), Text: line.Text}
			handleInput(event, c)
		}
	}
}
