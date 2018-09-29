package main

import (
	"bytes"
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
}

type CmdArgs []string

type WatchConfig struct {
	Name            string
	Patterns        []string
	CompiledRegexps []*regexp.Regexp
	Commands        [][]string
	Backoff         int
	IgnoreUntil     time.Time
	Channel         chan string
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

func execCommand(s string, args []string) {
	log.Printf("INFO exec: %v\n", args)
	cmd_name := args[0]
	opt_args := args[1:]
	cmd := exec.Command(cmd_name, opt_args...)
	cmd.Stdin = strings.NewReader(s)
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

func handleMatched(conf *WatchConfig) {
	for {
		s := <-conf.Channel
		if verbose {
			log.Println("DEBUG received:", s)
		}
		for _, args := range conf.Commands {
			go execCommand(s, args)
		}
	}
}

func handleInput(s string, conf *WatchConfig) {
	var match bool
	if len(conf.Patterns) > 0 {
		match = matchSimple(s, conf.Patterns)
	}
	if match == false && len(conf.CompiledRegexps) > 0 {
		match = matchRegexp(s, conf.CompiledRegexps)
	}
	if match == false {
		return
	}
	now := time.Now()
	if conf.Backoff > 0 {
		if now.Unix() > conf.IgnoreUntil.Unix() {
			conf.IgnoreUntil = now.Add(time.Second * time.Duration(conf.Backoff))
			log.Printf("INFO %s ignore event ultil %s\n", conf.Name, conf.IgnoreUntil.String())
		} else {
			// Ignore
			if verbose {
				log.Printf("DEBUG Skip matched event: %s\n", s)
			}
			return
		}
	}
	conf.Channel <- s
}

func main() {
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

		log.Printf("INFO RuleSet[%d] backoff: %d\n", i, rule.Backoff)
		conf.Backoff = rule.Backoff

		conf.Channel = make(chan string)

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
			handleInput(line.Text, c)
		}
	}
}
