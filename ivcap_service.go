package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"log"
)

const (
	MAX_ATTEMPTS = 10

	// referencing the otehr packages blows up Docker - keep an eye on this
	ARTIFACT_ID_HEADER            = "X-Artifact-Id"            // tus.ARTIFACT_HEADER
	CACHE_ID_HEADER               = "X-Cache-Id"               // data_proxy/pkg/caddy/cache/cache.go
	NAME_HEADER                   = "X-Name"                   // tus.NAME_HEADER
	META_DATA_FOR_ARTIFACT_HEADER = "X-Meta-Data-For-Artifact" // metadata.META_DATA_FOR_ARTIFACT_HEADER
	META_DATA_SCHEMA_HEADER       = "X-Meta-Data-Schema"       // metadata.META_DATA_SCHEMA_HEADER

	ORDER_ID_ENV    = "IVCAP_ORDER_ID"
	STORAGE_URL_ENV = "IVCAP_STORAGE_URL"
	STORAGE_URL_DEF = "http://localhost:8888"
	CACHE_URL_ENV   = "IVCAP_CACHE_URL"

	READYZ = "/readyz"
)

type EnvironmentNotReadyError struct{}

func (e *EnvironmentNotReadyError) Error() string {
	return "IVCAP environment doesn't seem to ready"
}

type ApiError struct {
	Message string
	Err     error
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("IVCAP API error - %s (%s)", e.Message, e.Err)
}

type HttpError struct {
	Request  *http.Request
	Response *http.Response
	Err      error
}

func (e *HttpError) Error() string {
	if e.Response == nil {
		return fmt.Sprintf("IVCAP: http request failed (%v) - %s", e.Request, e.Err)
	} else {
		return fmt.Sprintf("IVCAP: http response failed (%d) - %s", e.Response.StatusCode, e.Err)
	}
}

type Environment struct {
	localMode  bool
	noCaching  bool
	logger     LoggerI
	storageURL string
	cacheURL   string
}

type LoggerI interface {
	Error(format string, v ...any)
	Info(format string, v ...any)
	Debug(format string, v ...any)
}

// When used, run in local environment
func LocalMode(flag bool) func(r *Environment) {
	return func(e *Environment) {
		e.localMode = flag
	}
}

// When set, run in local environment
func Logger(logger LoggerI) func(r *Environment) {
	return func(e *Environment) {
		e.logger = logger
	}
}

// When used, do not use caching of external data
func NoCaching(flag bool) func(r *Environment) {
	return func(e *Environment) {
		e.noCaching = flag
	}
}

func NewEnvironment(options ...func(r *Environment)) (e *Environment) {
	e = &Environment{}
	for _, option := range options {
		option(e)
	}

	e.storageURL = getOptional(STORAGE_URL_ENV, STORAGE_URL_DEF)

	if !e.noCaching {
		e.cacheURL, _ = os.LookupEnv(CACHE_URL_ENV)
	}
	if e.logger == nil {
		e.logger = &baseLogger{}
	}
	return
}

func (e *Environment) WaitForEnvironmentReady(
	maxAttempts int,
) error {
	if e.localMode {
		return nil
	} else {
		return e.waitForEnvironmentReady(maxAttempts, 1)
	}
}

func (e *Environment) waitForEnvironmentReady(
	maxAttempts int,
	attempt int,
) error {
	endpoint := getOptional(STORAGE_URL_ENV, STORAGE_URL_DEF)

	url := endpoint + READYZ
	e.logger.Debug("checking 'ready' at '%s'", url)
	_, err := http.Get(url)
	if err != nil {
		if attempt > maxAttempts {
			err := &EnvironmentNotReadyError{}
			log.Print(err.Error())
			return err
		} else {
			delay := 10 * time.Second
			e.logger.Debug("Waiting for sidecars: attempt #%d delay: %d sec url: %s", attempt, delay/time.Second, endpoint)
			time.Sleep(delay)
			return e.waitForEnvironmentReady(maxAttempts, attempt+1)
		}
	}
	return nil
}

func (e *Environment) Publish(
	name string, // TODO needed?
	contentType string,
	reader io.Reader,
	meta interface{},
) (err error) {
	if e.localMode {
		b, _ := ioutil.ReadAll(reader)
		ioutil.WriteFile(name, b, 0644)
		return
	}

	url := e.storageURL + "/" + name
	req, err := http.NewRequest("PUT", url, reader)
	if err != nil {
		e.logger.Error("creating request failed - %s", err)
		return
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set(NAME_HEADER, name)
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		e.logger.Error("client.Do failed - %v", err)
		return &HttpError{req, res, err}
	} else if res.StatusCode >= 300 {
		e.logger.Error("save request failed - %d", res.StatusCode)
		return &HttpError{req, res, nil}
	}
	defer res.Body.Close()

	artifactID := res.Header.Get(ARTIFACT_ID_HEADER)
	e.logger.Info("Successfully uploaded object as '%s'", artifactID)
	if artifactID == "" {
		msg := fmt.Sprintf("Missing '%s' header", ARTIFACT_ID_HEADER)
		e.logger.Error(msg)
		return &ApiError{msg, nil}
	}
	e.logger.Debug("return headers from object upload - aid: %v h: %v", artifactID, res.Header)

	return e.PublishMetaForArtifact(name, artifactID, meta, url)
}

type WriteBodyF func(writer *io.PipeWriter) error

func (e *Environment) PublishAsync(
	name string, // TODO needed?
	contentType string,
	meta interface{},
	writeBodyF WriteBodyF,
) *sync.WaitGroup {
	pr, pw := io.Pipe()

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer func() {
			wg.Done()
		}()

		if err := writeBodyF(pw); err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
		e.logger.Debug("writing finished '%s'", name)
	}()
	go func() {
		defer wg.Done()
		err := e.Publish(name, contentType, pr, meta)
		pr.CloseWithError(err)
		e.logger.Debug("published '%s' - '%v'", name, err)
	}()
	return &wg
}

func (e *Environment) PublishMetaForArtifact(name string, artifactID string, meta interface{}, imgUrl string) (err error) {
	if meta == nil {
		return
	}

	jname := name + "-meta.json"
	url := e.storageURL + "/" + jname
	e.logger.Debug("starting to upload metadata - meta: %v url: %s", meta, url)
	body, _ := json.Marshal(meta)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		e.logger.Error("creating request failed - %s", err)
		return &HttpError{req, nil, err}
	}

	req.Header.Set("Content-Type", "application/json")

	req.Header.Set(META_DATA_FOR_ARTIFACT_HEADER, artifactID)
	req.Header.Set(META_DATA_SCHEMA_HEADER, "urn:schema:testing:image")
	req.Header.Set(NAME_HEADER, jname)

	client := &http.Client{}
	if resp, err := client.Do(req); err == nil {
		e.logger.Info("successfully upload metadata - status", resp.Status)
	} else {
		e.logger.Error("upload metadata failed - %s", err)
		return &HttpError{req, resp, err}
	}
	return
}

func (e *Environment) GetResource(url string, handler func(reader io.Reader) error) (err error) {
	if strings.HasPrefix(url, "urn:") {
		e.logger.Info("downloading artifact - urn: %s", url)
		url = e.storageURL + "/" + url
	} else {
		e.logger.Info("downloading remote content - url: %s, caching?: %t", url, e.cacheURL != "")
		if e.cacheURL != "" {
			url = fmt.Sprintf("%s/%s", e.cacheURL, base64.RawURLEncoding.EncodeToString([]byte(url)))
		}
	}

	response, err := http.Get(url)
	if err != nil {
		e.logger.Error("downloadImage: GET failed - url: %s, err: %s", url, err)
		err = &HttpError{nil, response, err}
		return
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		e.logger.Error("getting resource failed - statusCode: %d url: %s", response.StatusCode, url)
		err = &HttpError{nil, response, nil}
		return
	}
	e.logger.Debug("downloading '%s' succeeded - size: %s cache-id: %s", url,
		response.Header.Get("Content-Length"), response.Header.Get(CACHE_ID_HEADER))
	reader := response.Body
	err = handler(reader)
	return
}

func (e *Environment) GetOrderID() string {
	if oid, ok := os.LookupEnv(ORDER_ID_ENV); ok {
		return oid
	} else {
		return "???"
	}
}

func getOptional(key string, _default string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	} else {
		return _default
	}
}

//**** BASE LOGGER

type baseLogger struct{}

func (l *baseLogger) Error(format string, v ...any) {
	log.Printf("ERROR: "+format, v...)
}

func (l *baseLogger) Info(format string, v ...any) {
	log.Printf("INFO: "+format, v...)
}

func (l *baseLogger) Debug(format string, v ...any) {
	log.Printf("DEBUG: "+format, v...)
}
