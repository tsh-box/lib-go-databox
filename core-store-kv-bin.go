package libDatabox

import (
	"encoding/json"
	"errors"
	"strings"

	zest "github.com/toshbrown/goZestClient"
)

type BinaryKeyValue_0_3_0 interface {
	// Write text value to key
	Write(dataSourceID string, key string, payload []byte) error
	// Read text values from key.
	Read(dataSourceID string, key string) ([]byte, error)
	//ListKeys returns an array of key registed under the dataSourceID
	ListKeys(dataSourceID string) ([]string, error)
	// Get notifications of updated values for a key. Returns a channel that receives BinaryObserveResponse containing a JSON string when a new value is added.
	ObserveKey(dataSourceID string, key string) (<-chan BinaryObserveResponse, error)
	// Get notifications of updated values for any key. Returns a channel that receives BinaryObserveResponse containing a JSON string when a new value is added.
	Observe(dataSourceID string) (<-chan BinaryObserveResponse, error)
	// Get notifications of updated values
	RegisterDatasource(metadata DataSourceMetadata) error
}

type binaryKeyValueClient struct {
	zestClient         zest.ZestClient
	zestEndpoint       string
	zestDealerEndpoint string
}

// NewBinaryKeyValueClient returns a new NewBinaryKeyValueClient to enable reading and writing of binary data key value to the store
// reqEndpoint is provided in the DATABOX_ZMQ_ENDPOINT environment varable to databox apps and drivers.
func NewBinaryKeyValueClient(reqEndpoint string, enableLogging bool) (BinaryKeyValue_0_3_0, error) {

	kvc := binaryKeyValueClient{}
	kvc.zestEndpoint = reqEndpoint
	kvc.zestDealerEndpoint = strings.Replace(reqEndpoint, ":5555", ":5556", 1)
	thisKVC, err := zest.New(kvc.zestEndpoint, kvc.zestDealerEndpoint, getServerKey(), enableLogging)
	kvc.zestClient = thisKVC
	return kvc, err
}

func (kvc binaryKeyValueClient) Write(dataSourceID string, key string, payload []byte) error {
	path := "/kv/" + dataSourceID + "/" + key

	token, err := requestToken(kvc.zestEndpoint+path, "POST")
	if err != nil {
		return err
	}

	err = kvc.zestClient.Post(token, path, payload, "BINARY")
	if err != nil {
		invalidateCache(kvc.zestEndpoint+path, "POST")
		return errors.New("Error writing data: " + err.Error())
	}

	return nil
}

func (kvc binaryKeyValueClient) Read(dataSourceID string, key string) ([]byte, error) {
	path := "/kv/" + dataSourceID + "/" + key

	token, err := requestToken(kvc.zestEndpoint+path, "POST")
	if err != nil {
		return []byte(""), err
	}

	data, getErr := kvc.zestClient.Get(token, path, "BINARY")
	if getErr != nil {
		invalidateCache(kvc.zestEndpoint+path, "POST")
		return []byte(""), errors.New("Error reading data: " + getErr.Error())
	}

	return data, nil
}

func (kvc binaryKeyValueClient) ListKeys(dataSourceID string) ([]string, error) {
	path := "/kv/" + dataSourceID + "/keys"

	token, err := requestToken(kvc.zestEndpoint+path, "GET")
	if err != nil {
		return []string{}, err
	}

	data, getErr := kvc.zestClient.Get(token, path, "JSON")
	if getErr != nil {
		invalidateCache(kvc.zestEndpoint+path, "POST")
		return []string{}, errors.New("Error reading data: " + getErr.Error())
	}

	var keysArray []string

	err = json.Unmarshal(data, &keysArray)
	if err != nil {
		return []string{}, errors.New("Error decoding data: " + err.Error())
	}
	return keysArray, nil
}

func (kvc binaryKeyValueClient) ObserveKey(dataSourceID string, key string) (<-chan BinaryObserveResponse, error) {
	path := "/kv/" + dataSourceID + "/" + key

	token, err := requestToken(kvc.zestEndpoint+path, "GET")
	if err != nil {
		return nil, err
	}

	payloadChan, getErr := kvc.zestClient.Observe(token, path, "BINARY", 0)
	if getErr != nil {
		invalidateCache(kvc.zestEndpoint+path, "GET")
		return nil, errors.New("Error observing: " + getErr.Error())
	}

	objectChan := make(chan BinaryObserveResponse)

	go func() {
		for data := range payloadChan {

			ts, dsid, key, payload := parseRawObserveResponse(data)
			resp := BinaryObserveResponse{
				TimestampMS:  ts,
				DataSourceID: dsid,
				Key:          key,
				Data:         payload,
			}

			objectChan <- resp
		}

		//if we get here then payloadChan has been closed so close objectChan
		close(objectChan)
	}()

	return objectChan, nil
}

func (kvc binaryKeyValueClient) Observe(dataSourceID string) (<-chan BinaryObserveResponse, error) {
	path := "/kv/" + dataSourceID + "/*"

	token, err := requestToken(kvc.zestEndpoint+path, "GET")
	if err != nil {
		return nil, err
	}

	payloadChan, getErr := kvc.zestClient.Observe(token, path, "JSON", 0)
	if getErr != nil {
		invalidateCache(kvc.zestEndpoint+path, "GET")
		return nil, errors.New("Error observing: " + getErr.Error())
	}

	objectChan := make(chan BinaryObserveResponse)

	go func() {
		for data := range payloadChan {

			ts, dsid, key, payload := parseRawObserveResponse(data)
			resp := BinaryObserveResponse{
				TimestampMS:  ts,
				DataSourceID: dsid,
				Key:          key,
				Data:         payload,
			}

			objectChan <- resp
		}

		//if we get here then payloadChan has been closed so close objectChan
		close(objectChan)
	}()

	return objectChan, nil
}

func (kvc binaryKeyValueClient) RegisterDatasource(metadata DataSourceMetadata) error {

	path := "/cat"

	token, err := requestToken(kvc.zestEndpoint+path, "POST")
	if err != nil {
		return errors.New("Error getting token: " + err.Error())
	}
	hypercatJSON, err := dataSourceMetadataToHypercat(metadata, kvc.zestEndpoint+"/kv/")

	writeErr := kvc.zestClient.Post(token, path, hypercatJSON, "JSON")
	if writeErr != nil {
		invalidateCache(kvc.zestEndpoint+path, "POST")
		return errors.New("Error writing: " + writeErr.Error())
	}

	return nil
}
