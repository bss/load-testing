package main

import (
	"encoding/json"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"crypto/tls"
	"net"
	"net/url"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var verbose bool

type IntList []int

type TargetList []Target

type CookieList []string

type TemplatedValue string

type Config struct {
	Base    string
	Cookies CookieList
	Targets TargetList

	// Write and read delays in Millisecond
	WriteDelay time.Duration
	ReadDelay time.Duration
}

type Target struct {
	Method     string
	Path       string
	CookieLess bool
	Weight     int
	Payload    string
}

type Job struct {
	Method  string
	Cookie  string
	Url     string
	Payload string
	WriteDelay time.Duration
	ReadDelay time.Duration
}

func loadConfig(file string) (*Config, error) {
	var config *Config
	b, err := ioutil.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(b, &config)
	}
	return config, err
}

func (config *Config) newRandomJob() Job {
	target := config.Targets.getRandom()
	cookie := ""
	url := config.Base + target.Path
	if target.CookieLess != true {
		cookie = config.Cookies.getRandom()
	}
	job := Job{
		Method:  target.Method,
		Cookie:  cookie,
		Url:     url,
		Payload: target.Payload,
		WriteDelay: config.WriteDelay*time.Millisecond,
		ReadDelay: config.ReadDelay*time.Millisecond,
	}
	job.replaceTemplatedValues()
	return job
}

type SlowConn struct {
	net.Conn
	WriteDelay time.Duration
	ReadDelay time.Duration
}

func (conn *SlowConn) Write(data []byte) (int, error) {
	// Ok, we know this is some http shit.
	// The sleeping works by writing a line at a time to the
	// connection sleeping a bit in between.
	reader := bytes.NewBuffer(data)
	totalBytesWritten := 0
	for reader.Len() > 0 {
		line, _ := reader.ReadString('\n')
		time.Sleep(conn.WriteDelay)
		bytesWritten, err := conn.Conn.Write([]byte(line))
		totalBytesWritten = totalBytesWritten + bytesWritten
		if err != nil {
			return totalBytesWritten, err
		}
	}
	return totalBytesWritten, nil
}

func (conn *SlowConn) Read(data []byte) (int, error) {
	// Sleep a bit before we read stuff
	time.Sleep(conn.ReadDelay)
	return conn.Conn.Read(data)
}

func NewSlowDialer(writeDelay, readDelay time.Duration) (func(network, addr string) (net.Conn, error)) {
	return func(mynet, address string) (net.Conn, error) {
		conn, err := net.Dial(mynet, address)
		if err != nil {
			return nil, err
		}

		slowConn := &SlowConn{Conn: conn,
							  WriteDelay: writeDelay,
							  ReadDelay: readDelay }

		return slowConn, nil
	}
}

func (job *Job) run() {
	start_time := time.Now()
	req, _ := http.NewRequest(job.Method, job.Url, strings.NewReader(job.Payload))
	req.Header.Add("Content-type", "application/json") // Do not keep-alive connection
	req.Header.Add("Connection", "close") // Do not keep-alive connection
	if job.Cookie != "" {
		req.AddCookie(&http.Cookie{Name: "authentication", Value: job.Cookie})
	}
	client := &http.Client{
		Transport: &http.Transport{
			Dial:            NewSlowDialer(job.WriteDelay, job.ReadDelay),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	response, err := client.Do(req)

	if err != nil {
		log.Printf("Response error: %s\n", err)
	} else {
		// Make sure we actually read the body so we act like a real client
		// before closing the connection.
		defer response.Body.Close()
		ioutil.ReadAll(response.Body)
		if verbose {
			log.Printf("%s: %s %d %s (%s)\n", response.Status, job.Method, req.ContentLength, job.Url, time.Since(start_time))
		}
	}
}

var previousRandomValues IntList

func (job *Job) replaceTemplatedValues() {
	randVal := rand.Intn(100000000)
	previousRandomValues = previousRandomValues.appendShift(randVal)

	job.Url = strings.Replace(job.Url, "${STATIC_RANDOM_NUM}", strconv.Itoa(randVal), -1)
	job.Payload = strings.Replace(job.Payload, "${STATIC_RANDOM_NUM}", strconv.Itoa(randVal), -1)

	job.Url = replaceStr(job.Url, "${RANDOM_NUM}", func() string {
		return strconv.Itoa(rand.Intn(100000000))
	})
	job.Payload = replaceStr(job.Payload, "${RANDOM_NUM}", func() string {
		return strconv.Itoa(rand.Intn(100000000))
	})
	job.Url = replaceStr(job.Url, "${PREVIOUS_RANDOM_NUM}", func() string {
		return strconv.Itoa(previousRandomValues.getRandom())
	})
	job.Payload = replaceStr(job.Payload, "${PREVIOUS_RANDOM_NUM}", func() string {
		return strconv.Itoa(previousRandomValues.getRandom())
	})
}

func replaceStr(s string, old string, new func() string) string {
	for strings.Contains(s, old) {
		s = strings.Replace(s, old, new(), 1)
	}
	return s
}

func (list IntList) appendShift(newValue int) IntList {
	length := len(list)
	capacity := cap(list)
	if length < capacity {
		list = list[0 : length+1]
	} else {
		// Shift before append
		newSlice := make(IntList, length, capacity)
		copy(newSlice, list[1:length])
		list = newSlice
		length--
	}
	list[length] = newValue
	return list
}

func (list IntList) getRandom() int {
	if len(list) > 0 {
		return list[rand.Intn(len(list))]
	}
	return -1
}

func (cookies CookieList) getRandom() string {
	if len(cookies) > 0 {
		return cookies[rand.Intn(len(cookies))]
	}
	return ""
}

func (targets TargetList) getRandom() Target {
	// Returns a weighted random pick of targets
	runningTotal := 0
	weights := make(IntList, len(targets))

	for i, t := range targets {
		runningTotal += t.Weight
		weights[i] = runningTotal
	}
	// Only use weights if we actually have multiple targets
	if runningTotal > 0 {
		selectedWeight := rand.Intn(runningTotal)
		for i, running_weight := range weights {
			if selectedWeight+1 <= running_weight {
				return targets[i]
			}
		}
	}
	// If everything else fails, use the first target!
	return targets[0]
}

func logFinalStats(start_time time.Time, requests int, concurrency int) {
	elapsed := time.Since(start_time)
	elapsedByRequest := time.Duration(elapsed.Nanoseconds() / int64(requests) * int64(concurrency))
	log.Printf("Finalised %d requests in %s (avg. %s pr. request)\n", requests, elapsed, elapsedByRequest)
}

func main() {
	previousRandomValues = make([]int, 0, 5)
	rand.Seed(time.Now().Unix())
	var concurrency, requests int
	flag.BoolVar(&verbose, "v", false, "Enable verbose mode")
	flag.IntVar(&concurrency, "c", 10, "Concurrency of the load tester")
	flag.IntVar(&requests, "n", 1000, "The number of requests to make")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage %s [options] CONFIG_FILE\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	configFile := flag.Arg(0)
	if configFile == "" {
		log.Fatal("A config file must be supplied!")
	}

	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}
	baseURL, _ := url.Parse(config.Base)
	if !(strings.LastIndex(baseURL.Host, ":") > strings.LastIndex(baseURL.Host, "]")) {
		log.Fatal("The Base URL must contain the port used!")
	}
	if len(config.Targets) < 1 {
		log.Fatal("You must add some targets to the config!")
	}

	var wg sync.WaitGroup
	var jobs = make(chan Job)
	wg.Add(requests)
	start_time := time.Now()
	go func() {
		for i := 0; i < requests; i++ {
			jobs <- config.newRandomJob()
		}
	}()

	for i := 0; i < concurrency; i++ {
		go func() {
			for job := range jobs {
				job.run()
				wg.Done()
			}
		}()
	}
	wg.Wait()
	logFinalStats(start_time, requests, concurrency)
}
