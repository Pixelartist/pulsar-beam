package db

import (
	"errors"
	"time"

	"github.com/kafkaesque-io/pulsar-beam/src/model"

	log "github.com/sirupsen/logrus"
)

/**
 * An in memory database implmentation of the restful API data store
 * no data is persisted.
 * This is for testing only.
 */

// InMemoryHandler is the in memory cache driver
type InMemoryHandler struct {
	topics map[string]model.TopicConfig
	logger *log.Entry
}

//Init is a Db interface method.
func (s *InMemoryHandler) Init() error {
	s.logger = log.WithFields(log.Fields{"app": "inmemory-db"})
	s.topics = make(map[string]model.TopicConfig)
	return nil
}

//Sync is a Db interface method.
func (s *InMemoryHandler) Sync() error {
	return nil
}

//Health is a Db interface method
func (s *InMemoryHandler) Health() bool {
	return true
}

// Close closes database
func (s *InMemoryHandler) Close() error {
	return nil
}

//NewInMemoryHandler initialize a Mongo Db
func NewInMemoryHandler() (*InMemoryHandler, error) {
	handler := InMemoryHandler{}
	err := handler.Init()
	return &handler, err
}

// Create creates a new document
func (s *InMemoryHandler) Create(topicCfg *model.TopicConfig) (string, error) {
	key, err := getKey(topicCfg)
	if err != nil {
		return key, err
	}

	if _, ok := s.topics[key]; ok {
		return key, errors.New(DocAlreadyExisted)
	}

	topicCfg.Key = key
	topicCfg.CreatedAt = time.Now()
	topicCfg.UpdatedAt = topicCfg.CreatedAt

	s.topics[topicCfg.Key] = *topicCfg
	return key, nil
}

// GetByTopic gets a document by the topic name and pulsar URL
func (s *InMemoryHandler) GetByTopic(topicFullName, pulsarURL string) (*model.TopicConfig, error) {
	key, err := model.GetKeyFromNames(topicFullName, pulsarURL)
	if err != nil {
		return &model.TopicConfig{}, err
	}
	return s.GetByKey(key)
}

// GetByKey gets a document by the key
func (s *InMemoryHandler) GetByKey(hashedTopicKey string) (*model.TopicConfig, error) {
	if v, ok := s.topics[hashedTopicKey]; ok {
		return &v, nil
	}
	return &model.TopicConfig{}, errors.New(DocNotFound)
}

// Load loads the entire database as a list
func (s *InMemoryHandler) Load() ([]*model.TopicConfig, error) {
	results := []*model.TopicConfig{}
	for _, v := range s.topics {
		results = append(results, &v)
	}
	return results, nil
}

// Update updates or creates a topic config document
func (s *InMemoryHandler) Update(topicCfg *model.TopicConfig) (string, error) {
	key, err := getKey(topicCfg)
	if err != nil {
		return key, err
	}

	if _, ok := s.topics[key]; !ok {
		return s.Create(topicCfg)
	}

	v := s.topics[key]
	v.Token = topicCfg.Token
	v.Tenant = topicCfg.Tenant
	v.Notes = topicCfg.Notes
	v.TopicStatus = topicCfg.TopicStatus
	v.UpdatedAt = time.Now()
	v.Webhooks = topicCfg.Webhooks

	s.logger.Infof("upsert %s", key)
	s.topics[topicCfg.Key] = *topicCfg
	return key, nil

}

// Delete deletes a document
func (s *InMemoryHandler) Delete(topicFullName, pulsarURL string) (string, error) {
	key, err := model.GetKeyFromNames(topicFullName, pulsarURL)
	if err != nil {
		return "", err
	}
	return s.DeleteByKey(key)
}

// DeleteByKey deletes a document based on key
func (s *InMemoryHandler) DeleteByKey(hashedTopicKey string) (string, error) {
	if _, ok := s.topics[hashedTopicKey]; !ok {
		return "", errors.New(DocNotFound)
	}

	delete(s.topics, hashedTopicKey)
	return hashedTopicKey, nil
}
