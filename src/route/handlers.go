package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/kafkaesque-io/pulsar-beam/src/db"
	"github.com/kafkaesque-io/pulsar-beam/src/model"
	"github.com/kafkaesque-io/pulsar-beam/src/pulsardriver"
	"github.com/kafkaesque-io/pulsar-beam/src/util"

	log "github.com/sirupsen/logrus"
)

var singleDb db.Db

const subDelimiter = "-"

// Init initializes database
func Init() {
	singleDb = db.NewDbWithPanic(util.GetConfig().PbDbType)
}

// TokenServerResponse is the json object for token server response
type TokenServerResponse struct {
	Subject string `json:"subject"`
	Token   string `json:"token"`
}

// TokenSubjectHandler issues new token
func TokenSubjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	subject, ok := vars["sub"]
	if !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	if util.StrContains(util.SuperRoles, util.AssignString(r.Header.Get("injectedSubs"), "BOGUSROLE")) {
		tokenString, err := util.JWTAuth.GenerateToken(subject)
		if err != nil {
			util.ResponseErrorJSON(errors.New("failed to generate token"), w, http.StatusInternalServerError)
		} else {
			respJSON, err := json.Marshal(&TokenServerResponse{
				Subject: subject,
				Token:   tokenString,
			})
			if err != nil {
				util.ResponseErrorJSON(errors.New("failed to marshal token response json object"), w, http.StatusInternalServerError)
				return
			}
			w.Write(respJSON)
			w.WriteHeader(http.StatusOK)
		}
		return
	}
	util.ResponseErrorJSON(errors.New("incorrect subject"), w, http.StatusUnauthorized)
	return
}

// StatusPage replies with basic status code
func StatusPage(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	return
}

// ReceiveHandler - the message receiver handler
func ReceiveHandler(w http.ResponseWriter, r *http.Request) {
	b, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusInternalServerError)
		return
	}

	token, topicFN, pulsarURL, err2 := util.ReceiverHeader(util.AllowedPulsarURLs, &r.Header)
	if err2 != nil {
		util.ResponseErrorJSON(err2, w, http.StatusUnauthorized)
		return
	}
	log.Infof("topicFN %s pulsarURL %s", topicFN, pulsarURL)

	pulsarAsync := r.URL.Query().Get("mode") == "async"
	err = pulsardriver.SendToPulsar(pulsarURL, token, topicFN, b, pulsarAsync)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	return
}

// GetTopicHandler gets the topic details
func GetTopicHandler(w http.ResponseWriter, r *http.Request) {
	topicKey, err := GetTopicKey(r)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusUnprocessableEntity)
		return
	}

	// TODO: we may fix the problem that allows negatively look up by another tenant
	doc, err := singleDb.GetByKey(topicKey)
	if err != nil {
		log.Errorf("get topic error %v", err)
		util.ResponseErrorJSON(err, w, http.StatusNotFound)
		return
	}
	if !VerifySubjectBasedOnTopic(doc.TopicFullName, r.Header.Get("injectedSubs"), ExtractEvalTenant) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	resJSON, err := json.Marshal(doc)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusInternalServerError)
	} else {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		w.Write(resJSON)
	}

}

// UpdateTopicHandler creates or updates a topic
func UpdateTopicHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	var doc model.TopicConfig
	err := decoder.Decode(&doc)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusUnprocessableEntity)
		return
	}

	if _, err = model.ValidateTopicConfig(doc); err != nil {
		util.ResponseErrorJSON(err, w, http.StatusUnprocessableEntity)
		return
	}

	if !VerifySubjectBasedOnTopic(doc.TopicFullName, r.Header.Get("injectedSubs"), ExtractEvalTenant) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	id, err := singleDb.Update(&doc)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusConflict)
		return
	}
	if len(id) > 1 {
		savedDoc, err := singleDb.GetByKey(id)
		if err != nil {
			util.ResponseErrorJSON(err, w, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		resJSON, err := json.Marshal(savedDoc)
		if err != nil {
			util.ResponseErrorJSON(err, w, http.StatusInternalServerError)
			return
		}
		w.Write(resJSON)
		return
	}
	util.ResponseErrorJSON(fmt.Errorf("failed to update"), w, http.StatusInternalServerError)
	return
}

// DeleteTopicHandler deletes a topic
func DeleteTopicHandler(w http.ResponseWriter, r *http.Request) {
	topicKey, err := GetTopicKey(r)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusUnprocessableEntity)
		return
	}

	doc, err := singleDb.GetByKey(topicKey)
	if err != nil {
		log.Errorf("failed to get topic based on key %s err: %v", topicKey, err)
		util.ResponseErrorJSON(err, w, http.StatusNotFound)
		return
	}
	if !VerifySubjectBasedOnTopic(doc.TopicFullName, r.Header.Get("injectedSubs"), ExtractEvalTenant) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	deletedKey, err := singleDb.DeleteByKey(topicKey)
	if err != nil {
		util.ResponseErrorJSON(err, w, http.StatusNotFound)
		return
	}
	resJSON, err := json.Marshal(deletedKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		w.Write(resJSON)
	}
}

// GetTopicKey gets the topic key from the request body or url sub route
func GetTopicKey(r *http.Request) (string, error) {
	var err error
	vars := mux.Vars(r)
	topicKey, ok := vars["topicKey"]
	if !ok {
		var topic model.TopicKey
		decoder := json.NewDecoder(r.Body)
		defer r.Body.Close()

		err := decoder.Decode(&topic)
		switch {
		case err == io.EOF:
			return "", errors.New("missing topic key or topic names in body")
		case err != nil:
			return "", err
		}
		topicKey, err = model.GetKeyFromNames(topic.TopicFullName, topic.PulsarURL)
		if err != nil {
			return "", err
		}
	}
	return topicKey, err
}

// VerifySubjectBasedOnTopic verifies the subject can meet the requirement.
func VerifySubjectBasedOnTopic(topicFN, tokenSub string, evalTenant func(tenant, subjects string) bool) bool {
	parts := strings.Split(topicFN, "/")
	if len(parts) < 4 {
		return false
	}
	tenant := parts[2]
	if len(tenant) < 1 {
		log.Infof(" auth verify tenant %s token sub %s", tenant, tokenSub)
		return false
	}
	return VerifySubject(tenant, tokenSub, evalTenant)
}

// VerifySubject verifies the subject can meet the requirement.
// Subject verification requires role or tenant name in the jwt subject
func VerifySubject(requiredSubject, tokenSubjects string, evalTenant func(tenant, subjects string) bool) bool {
	for _, v := range strings.Split(tokenSubjects, ",") {
		if util.StrContains(util.SuperRoles, v) {
			return true
		}
		if requiredSubject == v {
			return true
		}
		if evalTenant(requiredSubject, v) {
			return true
		}
	}

	return false
}

// ExtractEvalTenant is a customized function to evaluate subject against tenant
func ExtractEvalTenant(requiredSubject, tokenSub string) bool {
	// expect - in subject unless it is superuser
	var sub string
	parts := strings.Split(tokenSub, subDelimiter)
	if len(parts) < 2 {
		sub = parts[0]
	}

	validLength := len(parts) - 1
	sub = strings.Join(parts[:validLength], subDelimiter)
	if sub != "" && requiredSubject == sub {
		return true
	}
	return false
}
