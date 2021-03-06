package k8s

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"gitlab.com/tslocum/cview"
	cv1 "k8s.io/api/core/v1"
)

type ErrMultipleContainers struct {
	error

	Containers []string
}

type logData struct {
	color []byte
	from  []byte
	line  []byte
	time  time.Time
}

const previousPrefix = "previous:"

func (c *Client) Logs(ctx context.Context, object ObjectMetaGetter, container string, colors []string) (<-chan []byte, error) {
	var pods []*cv1.Pod

	switch v := object.(type) {
	case *cv1.Pod:
		pods = append(pods, v)
	case PodManager:
		pods = v.Pods()
	}

	if len(pods) == 0 {
		return nil, nil
	}

	var previous bool
	if strings.HasPrefix(container, previousPrefix) {
		previous = true
		container = container[len(previousPrefix):]
	}

	names := make([]string, 0, len(pods[0].Status.ContainerStatuses))
	for _, c := range pods[0].Status.ContainerStatuses {
		if c.Name == container {
			names = nil
			break
		}
		names = append(names, c.Name)
		if c.LastTerminationState.Terminated != nil {
			names = append(names, previousPrefix+c.Name)
		}

	}
	if len(names) > 1 {
		return nil, ErrMultipleContainers{
			errors.New("multiple containers"),
			names,
		}
	}

	writer := make(chan []byte)
	reader := make(chan logData)

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	go demuxLogs(ctx, writer, reader, len(pods) > 1)

	var wg sync.WaitGroup
	for i, pod := range pods {
		name := pod.ObjectMeta.GetName()
		req := c.CoreV1().Pods(pod.ObjectMeta.GetNamespace()).GetLogs(
			name, &cv1.PodLogOptions{Previous: previous, Follow: true, Container: container, Timestamps: true})
		rc, err := req.Stream(ctx)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("getting logs for pod %s: %w", name, NormalizeError(err))
		}

		prefix := name
		idx := strings.LastIndex(name, "-")
		if idx > 0 {
			prefix = name[idx+1:]
		}

		wg.Add(1)
		go func(i int) {
			readLogData(ctx, rc, reader, []byte(prefix), []byte(colors[i%len(colors)]))
			wg.Done()
		}(i)
	}

	go func() {
		wg.Wait()
		close(reader)
		cancel()
	}()

	return writer, nil
}

func demuxLogs(ctx context.Context, writer chan<- []byte, reader <-chan logData, showPrefixes bool) {
	var logData []logData
	var buf bytes.Buffer
	canTrigger := true
	trig := make(chan struct{})

	defer close(writer)
	for {
		select {
		case <-ctx.Done():
			return
		case <-trig:
			sort.Slice(logData, func(i, j int) bool {
				return logData[i].time.Before(logData[j].time)
			})
			for _, d := range logData {
				if showPrefixes {
					buf.Write([]byte("["))
					buf.Write(d.color)
					buf.Write([]byte("]"))
					buf.Write(d.from)
					buf.Write([]byte(" → "))
					buf.Write([]byte("[white]"))
				}

				buf.Write([]byte(cview.Escape(string(d.line))))
			}
			logData = nil

			writer <- buf.Bytes()
			buf.Reset()
			canTrigger = true
		case data, open := <-reader:
			if !open {
				reader = nil
				continue
			}
			logData = append(logData, data)
			// Buffer the writes in a timed window to avoid having to print out
			// line by line when there is a lot of initial content
			if canTrigger {
				time.AfterFunc(500*time.Millisecond, func() { trig <- struct{}{} })
				canTrigger = false
			}
		}
	}
}

func readLogData(ctx context.Context, rc io.ReadCloser, data chan<- logData, prefix []byte, color []byte) {
	defer rc.Close()

	r := bufio.NewReader(rc)
	for {
		if ctx.Err() != nil {
			return
		}
		b, err := r.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading stream: %v", err)
				return
			}
			return
		}

		parts := bytes.SplitN(b, []byte(" "), 2)
		t, err := time.Parse(time.RFC3339Nano, string(parts[0]))
		if err != nil {
			log.Printf("Error parsing time %s from log line: %v", string(parts[0]), err)
		}

		data <- logData{from: prefix, color: color, line: parts[1], time: t}
	}
}
