// Copyright 2019 The nemanjamikic Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package kafka implements a Kafka reporter to send spans to a Kafka server/cluster.
*/
package kafka

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/Shopify/sarama"
	"github.com/nemanjamikic/zipkin-go/model"
	"github.com/nemanjamikic/zipkin-go/reporter"
)

// defaultKafkaTopic sets the standard Kafka topic our Reporter will publish
// on. The default topic for zipkin-receiver-kafka is "zipkin", see:
// https://github.com/nemanjamikic/zipkin/tree/master/zipkin-receiver-kafka
const defaultKafkaTopic = "zipkin"

// kafkaReporter implements Reporter by publishing spans to a Kafka
// broker.
type kafkaReporter struct {
	producer           sarama.AsyncProducer
	logger             *log.Logger
	topic              string
	serializer         reporter.SpanSerializer
	nonBlockingTimeout time.Duration
}

// ReporterOption sets a parameter for the kafkaReporter
type ReporterOption func(c *kafkaReporter)

// Logger sets the logger used to report errors in the collection
// process.
func Logger(logger *log.Logger) ReporterOption {
	return func(c *kafkaReporter) {
		c.logger = logger
	}
}

// Producer sets the producer used to produce to Kafka.
func Producer(p sarama.AsyncProducer) ReporterOption {
	return func(c *kafkaReporter) {
		c.producer = p
	}
}

// Topic sets the kafka topic to attach the reporter producer on.
func Topic(t string) ReporterOption {
	return func(c *kafkaReporter) {
		c.topic = t
	}
}

// Serializer sets the serialization function to use for sending span data to
// Zipkin.
func Serializer(serializer reporter.SpanSerializer) ReporterOption {
	return func(c *kafkaReporter) {
		if serializer != nil {
			c.serializer = serializer
		}
	}
}

// AsyncSendTimeout enables and sets timeout for non-blocking sending data
func AsyncSendTimeout(duration time.Duration) ReporterOption {
	return func(c *kafkaReporter) {
		c.nonBlockingTimeout = duration
	}
}

// NewReporter returns a new Kafka-backed Reporter. address should be a slice of
// TCP endpoints of the form "host:port".
func NewReporter(address []string, options ...ReporterOption) (reporter.Reporter, error) {
	r := &kafkaReporter{
		logger:             log.New(os.Stderr, "", log.LstdFlags),
		topic:              defaultKafkaTopic,
		serializer:         reporter.JSONSerializer{},
		nonBlockingTimeout: -1,
	}

	for _, option := range options {
		option(r)
	}
	if r.producer == nil {
		p, err := sarama.NewAsyncProducer(address, nil)
		if err != nil {
			return nil, err
		}
		r.producer = p
	}

	go r.logErrors()

	return r, nil
}

func (r *kafkaReporter) logErrors() {
	for pe := range r.producer.Errors() {
		r.logger.Print("msg", pe.Msg, "err", pe.Err, "result", "failed to produce msg")
	}
}

func (r *kafkaReporter) Send(s model.SpanModel) {
	// Zipkin expects the message to be wrapped in an array
	ss := []model.SpanModel{s}
	m, err := json.Marshal(ss)
	if err != nil {
		r.logger.Printf("failed when marshalling the span: %s\n", err.Error())
		return
	}
	msg := &sarama.ProducerMessage{
		Topic: r.topic,
		Key:   nil,
		Value: sarama.ByteEncoder(m),
	}

	// check if non-blocking send is allowed
	if r.nonBlockingTimeout >= 0 {
		select {
		case r.producer.Input() <- msg:
			return
		case <-time.After(r.nonBlockingTimeout):
			r.logger.Printf("failed to send msg beaceuse chan is full, msg %s\n", msg.Value)
			return
		}
	} else {
		r.producer.Input() <- msg
	}
}

func (r *kafkaReporter) Close() error {
	return r.producer.Close()
}
