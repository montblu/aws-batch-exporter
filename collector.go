package awsBatchExporter

import (
	"context"
	"log"
	"sync"
	"time"
	"encoding/json"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/batch"
	"github.com/aws/aws-sdk-go/service/batch/batchiface"
	"github.com/prometheus/client_golang/prometheus"
)

type Collector struct {
	client  batchiface.BatchAPI
	region  string
	timeout time.Duration
}

const (
	namespace = "aws_batch"
	timeout   = 10 * time.Second
)

var (
	jobStatus = []string{
		batch.JobStatusSubmitted,
		batch.JobStatusPending,
		batch.JobStatusRunnable,
		batch.JobStatusStarting,
		batch.JobStatusRunning,
		batch.JobStatusFailed,
		batch.JobStatusSucceeded,
	}

	jobSubmitted = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "submitted_job"),
		"Job in the queue that are in the SUBMITTED state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobPending = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "pending_job"),
		"Job in the queue that are in the PENDING state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobRunnable = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "runnable_job"),
		"Job in the queue that are in the RUNNABLE state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobStarting = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "starting_job"),
		"Job in the queue that are in the STARTING state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobRunning = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "running_job"),
		"Job in the queue that are in the RUNNING state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobFailed = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "failed_job"),
		"Job in the queue that are in the FAILED state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobSucceeded = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "succeeded_job"),
		"Job in the queue that are in the SUCCEEDED state",
		[]string{"region", "id", "queue", "name", "definition"}, nil,
	)

	jobDescMap = map[string]*prometheus.Desc{
		batch.JobStatusSubmitted: jobSubmitted,
		batch.JobStatusPending:   jobPending,
		batch.JobStatusRunnable:  jobRunnable,
		batch.JobStatusStarting:  jobStarting,
		batch.JobStatusRunning:   jobRunning,
		batch.JobStatusFailed:    jobFailed,
		batch.JobStatusSucceeded: jobSucceeded,
	}
)

type JobResult struct {
	id         string
	queue      string
	name       string
	status     string
	definition string
}

func New(region string) (*Collector, error) {
	s, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	if err != nil {
		return nil, err
	}

	return &Collector{
		client:  batch.New(s),
		region:  region,
		timeout: timeout,
	}, nil
}

func (*Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- jobSubmitted
	ch <- jobPending
	ch <- jobRunnable
	ch <- jobStarting
	ch <- jobRunning
	ch <- jobFailed
	ch <- jobSucceeded
}

// Function to extract the JobDefinition name
func getJobDefinitionSubstring(jobDefinitionArn string) string {
	// Look for the substring after "job-definition/"
	startIndex := strings.Index(jobDefinitionArn, "job-definition/")
	if startIndex == -1 {
		log.Printf("Invalid ARN: %s, 'job-definition/' not found", jobDefinitionArn)
		return ""
	}

	// Move the index to the end of "job-definition/"
	startIndex += len("job-definition/")

	// Find the colon (:) indicating the start of the revision number
	endIndex := strings.Index(jobDefinitionArn[startIndex:], ":")
	if endIndex == -1 {
		log.Printf("Invalid ARN: %s, revision number not found", jobDefinitionArn)
		return ""
	}

	// Substring from startIndex to endIndex
	return jobDefinitionArn[startIndex : startIndex+endIndex]
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := c.client.DescribeJobQueuesWithContext(ctx, &batch.DescribeJobQueuesInput{})
	if err != nil {
		log.Printf("Error collecting metrics: %v\n", err)
		return
	}
	var wg sync.WaitGroup
	for _, d := range r.JobQueues {
		wg.Add(1)
		go func(d batch.JobQueueDetail) {
			defer wg.Done()
			var results []JobResult
			for _, s := range jobStatus {
				r, err := c.client.ListJobsWithContext(ctx, &batch.ListJobsInput{JobQueue: d.JobQueueName, JobStatus: &s})
				if err != nil {
					log.Printf("Error collecting job status metrics: %v\n", err)
					continue
				}
				for _, j := range r.JobSummaryList {
					// Use DescribeJobs for each JobId to get detailed info, including the JobDefinition
                    describeRes, err := c.client.DescribeJobsWithContext(ctx, &batch.DescribeJobsInput{
                        Jobs: []*string{j.JobId},
                    })
                    if err != nil {
                        log.Printf("Error fetching job details for JobId %s: %v\n", *j.JobId, err)
                        continue
                    }

					data, err := json.Marshal(describeRes)
					if err != nil {
						log.Printf("Error marshaling describeRes: %v", err)
					} else {
						log.Printf("Describe Response: %s", string(data))
					}

					// Get JobDefinition from detailed result
					definition := "undefined"
					if len(describeRes.Jobs) > 0 && describeRes.Jobs[0].JobDefinition != nil {
						// Get simplified definition out of the ARN
						definition = getJobDefinitionSubstring(*describeRes.Jobs[0].JobDefinition)
					}

					results = append(results, JobResult{id: *j.JobId, queue: *d.JobQueueName, name: *j.JobName, status: *j.Status, definition: definition})
				}
			}
			c.collectJobDetailStatus(ch, results)
		}(*d)
	}
	wg.Wait()
}

func (c *Collector) collectJobDetailStatus(ch chan<- prometheus.Metric, results []JobResult) {
	for _, r := range results {
		ch <- prometheus.MustNewConstMetric(jobDescMap[r.status], prometheus.GaugeValue, 1, c.region, r.id, r.queue, r.name, r.definition)
	}
}
