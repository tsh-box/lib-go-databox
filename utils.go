//
// A golang library for interfacing with Databox APIs.
//
// Install using go get github.com/me-box/lib-go-databox
//
// Examples can be found in the samples directory
//
package libDatabox

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	s "strings"
	"time"
)

var hostname = os.Getenv("DATABOX_LOCAL_NAME")
var arbiterURL = os.Getenv("DATABOX_ARBITER_ENDPOINT")
var arbiterToken string

var databoxClient *http.Client
var databoxTlsConfig *tls.Config

func init() {

	//get the arbiterToken
	arbToken, err := ioutil.ReadFile("/run/secrets/ARBITER_TOKEN")
	if err != nil {
		panic("failed to read ARBITER_TOKEN")
	}
	arbiterToken = b64.StdEncoding.EncodeToString([]byte(arbToken))

	//setup the https root cert
	CM_HTTPS_CA_ROOT_CERT, err := ioutil.ReadFile("/run/secrets/DATABOX_ROOT_CA")
	if err != nil {
		panic("failed to read root certificate")
	}
	roots := x509.NewCertPool()
	ok := roots.AppendCertsFromPEM([]byte(CM_HTTPS_CA_ROOT_CERT))
	if !ok {
		panic("failed to parse root certificate")
	}

	databoxTlsConfig = &tls.Config{RootCAs: roots}
	tr := &http.Transport{
		TLSClientConfig: databoxTlsConfig,
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableCompression:  true,
	}

	databoxClient = &http.Client{
		Transport: tr,
		Timeout:   time.Second * 10,
	}

}

func getDataboxTslConfig() *tls.Config {
	return databoxTlsConfig
}

//GetHttpsCredentials Returns a string containing the HTTPS credentials to pass to https server when offering an https server.
//These are read form /run/secrets/DATABOX.pem and are generated by the container-manger at run time.
func GetHttpsCredentials() string {
	return string("/run/secrets/DATABOX.pem")
}

//JsonUnmarshal is a helper function to translate JSON stringified environment variable
//to go map[string]
func JsonUnmarshal(s string) (map[string]interface{}, error) {

	byt := []byte(s)
	var dat map[string]interface{}
	if err := json.Unmarshal(byt, &dat); err != nil {
		return nil, err
	}

	return dat, nil
}

// GetStoreURLFromDsHref extracts the base store url from the href provied in the DATASOURCE_[name] environment variable.
func GetStoreURLFromDsHref(href string) (string, error) {

	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}

	return u.Scheme + "://" + u.Host, nil

}

// GetDsIdFromDsHref extracts the base data source ID from the href provied in the DATASOURCE_[name] environment variable.
func GetDsIdFromDsHref(href string) (string, error) {

	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}

	return s.Replace(u.Path, "/", "", -1), nil

}

func makeArbiterRequest(arbMethod string, path string, hostname string, endpoint string, method string) (string, int) {

	var jsonStr = []byte(`{"target":"` + hostname + `","path":"` + endpoint + `","method":"` + method + `"}`)

	fmt.Println(string(jsonStr[:]))

	url := arbiterURL + path

	req, err := http.NewRequest(arbMethod, url, bytes.NewBuffer(jsonStr))
	req.Header.Set("X-Api-Key", arbiterToken)
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	resp, err := databoxClient.Do(req)
	if err != nil {
		return err.Error(), 503
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	return string(body[:]), resp.StatusCode
}

func requestToken(href string, method string) (string, error) {

	u, err := url.Parse(href)
	if err != nil {
		return href, err
	}

	host, _, err1 := net.SplitHostPort(u.Host)
	if err != nil {
		return href, err1
	}

	token, status := makeArbiterRequest("POST", "/token", host, u.Path, method)

	if status != 200 {
		err = errors.New(strconv.Itoa(status) + ": " + token)
	}

	return token, err
}

var tokenCache = make(map[string]string)

func makeStoreRequest(href string, method string) (string, error) {

	method = s.ToUpper(method)
	routeHash := s.ToUpper(href) + method

	_, exists := tokenCache[routeHash]
	if !exists {
		//request a token
		fmt.Println("Token not in cache requesting new one")
		newToken, err := requestToken(href, method)
		if err != nil {
			return "", err
		}
		tokenCache[routeHash] = newToken
	}

	//perform store request with token
	req, err := http.NewRequest(method, href, nil)
	req.Header.Set("X-Api-Key", tokenCache[routeHash])
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	resp, err := databoxClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err1 := ioutil.ReadAll(resp.Body)
	if err1 != nil {
		return "", err1
	}

	return string(body[:]), nil
}

func makeStoreRequestPOST(href string, data string) (string, error) {

	method := "POST"
	routeHash := s.ToUpper(href) + method

	_, exists := tokenCache[routeHash]
	if !exists {
		//request a token
		fmt.Println("Token not in cache requesting new one")
		newToken, err := requestToken(href, method)
		if err != nil {
			return "", err
		}
		tokenCache[routeHash] = newToken
	}

	//perform store request with token
	req, err := http.NewRequest(method, href, bytes.NewBufferString(data))
	req.Header.Set("X-Api-Key", tokenCache[routeHash])
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	resp, err := databoxClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err1 := ioutil.ReadAll(resp.Body)
	if err1 != nil {
		return "", err1
	}

	return string(body[:]), nil
}

//WaitForStoreStatus will wait for the store available at href to respond with an active status.
func WaitForStoreStatus(href string) {

	h, err := GetStoreURLFromDsHref(href)

	if err == nil {
		resp, err1 := databoxClient.Get(h + "/status")
		if err1 == nil {
			defer resp.Body.Close()
			_, err = ioutil.ReadAll(resp.Body)
		} else {
			err = err1
		}
	}

	if err != nil {
		fmt.Printf("[waitForStoreStatus] Retrying in 1s...")
		time.Sleep(1000 * time.Millisecond)
		WaitForStoreStatus(href)
	}

}

type StoreMetadata struct {
	Description    string
	ContentType    string
	Vendor         string
	DataSourceType string
	DataSourceID   string
	StoreType      string
	IsActuator     bool
	Unit           string
	Location       string
}

type relValPair struct {
	Rel string `json:"rel"`
	Val string `json:"val"`
}

type relValPairBool struct {
	Rel string `json:"rel"`
	Val bool   `json:"val"`
}

type hypercat struct {
	ItemMetadata []interface{} `json:"item-metadata"`
	Href         string        `json:"href"`
}

// RegisterDatasource is used by apps and drivers to register datasource in stores they
// own.
func RegisterDatasource(href string, metadata StoreMetadata) (string, error) {

	catURL, _ := GetStoreURLFromDsHref(href)
	catURL = catURL + "/cat"

	if metadata.Description == "" ||
		metadata.ContentType == "" ||
		metadata.Vendor == "" ||
		metadata.DataSourceType == "" ||
		metadata.DataSourceID == "" ||
		metadata.StoreType == "" {

		return "", errors.New("Missing required metadata")

	}

	cat := hypercat{}
	cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-hypercat:rels:hasDescription:en", Val: metadata.Description})
	cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-hypercat:rels:isContentType", Val: metadata.ContentType})
	cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-databox:rels:hasVendor", Val: metadata.Vendor})
	cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-databox:rels:hasType", Val: metadata.DataSourceType})
	cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-databox:rels:hasDatasourceid", Val: metadata.DataSourceID})
	cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-databox:rels:hasStoreType", Val: metadata.StoreType})

	if metadata.IsActuator {
		cat.ItemMetadata = append(cat.ItemMetadata, relValPairBool{Rel: "urn:X-databox:rels:isActuator", Val: true})
	}

	if metadata.Location != "" {
		cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-databox:rels:hasLocation", Val: metadata.Location})
	}

	if metadata.Unit != "" {
		cat.ItemMetadata = append(cat.ItemMetadata, relValPair{Rel: "urn:X-databox:rels:hasUnit", Val: metadata.Unit})
	}

	href, _ = GetStoreURLFromDsHref(href)
	cat.Href = href + "/" + metadata.DataSourceID

	jsonByteArray, _ := json.Marshal(cat)

	fmt.Println(string(jsonByteArray[:]))

	return makeStoreRequestPOST(catURL, string(jsonByteArray[:]))
}
