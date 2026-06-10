package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	DefaultTimeoutMS = int64(10000)
	DefaultFindLimit = int64(100)
	MaxFindLimit     = int64(1000)
)

type PluginRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type MongoArgs struct {
	Client     int64  `json:"client"`
	URI        string `json:"uri"`
	Database   string `json:"database"`
	Collection string `json:"collection"`

	Filter    json.RawMessage   `json:"filter"`
	Document  json.RawMessage   `json:"document"`
	Documents []json.RawMessage `json:"documents"`
	Update    json.RawMessage   `json:"update"`
	Keys      json.RawMessage   `json:"keys"`

	Limit                    int64  `json:"limit"`
	Skip                     int64  `json:"skip"`
	TimeoutMS                int64  `json:"timeoutMs"`
	MaxPoolSize              uint64 `json:"maxPoolSize"`
	MinPoolSize              uint64 `json:"minPoolSize"`
	ServerSelectionTimeoutMS int64  `json:"serverSelectionTimeoutMs"`
	ConnectTimeoutMS         int64  `json:"connectTimeoutMs"`

	AppName string `json:"appName"`
	Name    string `json:"name"`
	Unique  bool   `json:"unique"`
	Upsert  bool   `json:"upsert"`

	Sort       json.RawMessage `json:"sort"`
	Projection json.RawMessage `json:"projection"`
}

var (
	mu      sync.Mutex
	clients       = map[int64]*mongo.Client{}
	nextID  int64 = 1
)

func main() {}

func timeoutMS(ms int64) int64 {
	if ms <= 0 {
		return DefaultTimeoutMS
	}
	return ms
}

func makeContext(ms int64) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(timeoutMS(ms))*time.Millisecond)
}

func okJSON(s string) *C.char {
	return C.CString(s)
}

func errorJSON(kind, message string) *C.char {
	out, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"kind":    kind,
			"message": message,
		},
	})

	return C.CString(string(out))
}

func parseRequest(payload string) (PluginRequest, error) {
	var req PluginRequest
	err := json.Unmarshal([]byte(payload), &req)
	return req, err
}

func parseArg(req PluginRequest) (MongoArgs, error) {
	if len(req.Args) < 1 {
		return MongoArgs{}, fmt.Errorf("missing args[0]")
	}

	var arg MongoArgs
	err := json.Unmarshal(req.Args[0], &arg)
	return arg, err
}

func parseDoc(raw json.RawMessage) (bson.M, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return bson.M{}, nil
	}

	var doc bson.M
	err := bson.UnmarshalExtJSON(raw, false, &doc)
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func parseOrderedDoc(raw json.RawMessage) (bson.D, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return bson.D{}, nil
	}

	var doc bson.D
	err := bson.UnmarshalExtJSON(raw, false, &doc)
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func parseDocs(rawDocs []json.RawMessage) ([]any, error) {
	docs := make([]any, 0, len(rawDocs))

	for _, raw := range rawDocs {
		doc, err := parseDoc(raw)
		if err != nil {
			return nil, err
		}

		docs = append(docs, doc)
	}

	return docs, nil
}

func decodeJSONWithNumbers(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	err := decoder.Decode(&value)
	return value, err
}

func normalizeExtJSON(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))

		for i, item := range v {
			out[i] = normalizeExtJSON(item)
		}

		return out

	case map[string]any:
		if len(v) == 1 {
			if oid, ok := v["$oid"].(string); ok {
				return map[string]any{
					"id": oid,
				}
			}

			if date, ok := v["$date"].(string); ok {
				return map[string]any{
					"date": date,
				}
			}

			if dateObj, ok := v["$date"].(map[string]any); ok {
				if n, ok := dateObj["$numberLong"].(json.Number); ok {
					return map[string]any{
						"date": n.String(),
					}
				}

				if s, ok := dateObj["$numberLong"].(string); ok {
					return map[string]any{
						"date": s,
					}
				}

				return map[string]any{
					"date": normalizeExtJSON(dateObj),
				}
			}

			if decimal, ok := v["$numberDecimal"].(string); ok {
				return map[string]any{
					"decimal": decimal,
				}
			}

			if binary, ok := v["$binary"].(map[string]any); ok {
				return map[string]any{
					"binary": normalizeExtJSON(binary),
				}
			}

			if regex, ok := v["$regularExpression"].(map[string]any); ok {
				return map[string]any{
					"regex": normalizeExtJSON(regex),
				}
			}

			if timestamp, ok := v["$timestamp"].(map[string]any); ok {
				return map[string]any{
					"timestamp": normalizeExtJSON(timestamp),
				}
			}
		}

		out := map[string]any{}

		for key, item := range v {
			out[key] = normalizeExtJSON(item)
		}

		return out

	default:
		return value
	}
}

func extJSONToTinyJSON(data []byte) (string, error) {
	value, err := decodeJSONWithNumbers(data)
	if err != nil {
		return "", err
	}

	normalized := normalizeExtJSON(value)

	out, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func jsonValue(value any) (string, error) {
	wrapped := bson.M{
		"value": value,
	}

	data, err := bson.MarshalExtJSON(wrapped, false, false)
	if err != nil {
		return "", err
	}

	var decoded map[string]json.RawMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		return "", err
	}

	return extJSONToTinyJSON(decoded["value"])
}

func jsonDoc(value any) (string, error) {
	data, err := bson.MarshalExtJSON(value, false, false)
	if err != nil {
		return "", err
	}

	return extJSONToTinyJSON(data)
}

func saveClient(client *mongo.Client) int64 {
	mu.Lock()
	defer mu.Unlock()

	id := nextID
	nextID++

	clients[id] = client
	return id
}

func getClient(id int64) (*mongo.Client, error) {
	mu.Lock()
	defer mu.Unlock()

	client := clients[id]
	if client == nil {
		return nil, fmt.Errorf("invalid mongo client handle")
	}

	return client, nil
}

func deleteClient(id int64) {
	mu.Lock()
	defer mu.Unlock()

	delete(clients, id)
}

func snapshotClients() map[int64]*mongo.Client {
	mu.Lock()
	defer mu.Unlock()

	out := map[int64]*mongo.Client{}

	for id, client := range clients {
		out[id] = client
	}

	return out
}

func clearClients() {
	mu.Lock()
	defer mu.Unlock()

	clients = map[int64]*mongo.Client{}
}

func collectionFromArg(arg MongoArgs) (*mongo.Collection, error) {
	if arg.Database == "" {
		return nil, fmt.Errorf("missing database")
	}

	if arg.Collection == "" {
		return nil, fmt.Errorf("missing collection")
	}

	client, err := getClient(arg.Client)
	if err != nil {
		return nil, err
	}

	return client.Database(arg.Database).Collection(arg.Collection), nil
}

func applyFindOptions(arg MongoArgs) (*options.FindOptionsBuilder, error) {
	limit := arg.Limit

	if limit == 0 {
		limit = DefaultFindLimit
	}

	if limit < 0 {
		return nil, fmt.Errorf("limit cannot be negative")
	}

	if limit > MaxFindLimit {
		return nil, fmt.Errorf("limit too large; max is %d", MaxFindLimit)
	}

	if arg.Skip < 0 {
		return nil, fmt.Errorf("skip cannot be negative")
	}

	opts := options.Find().SetLimit(limit)

	if arg.Skip > 0 {
		opts.SetSkip(arg.Skip)
	}

	if len(arg.Sort) > 0 && string(arg.Sort) != "null" {
		sort, err := parseOrderedDoc(arg.Sort)
		if err != nil {
			return nil, fmt.Errorf("invalid sort: %w", err)
		}

		opts.SetSort(sort)
	}

	if len(arg.Projection) > 0 && string(arg.Projection) != "null" {
		projection, err := parseDoc(arg.Projection)
		if err != nil {
			return nil, fmt.Errorf("invalid projection: %w", err)
		}

		opts.SetProjection(projection)
	}

	return opts, nil
}

func applyFindOneOptions(arg MongoArgs) (*options.FindOneOptionsBuilder, error) {
	opts := options.FindOne()

	if len(arg.Sort) > 0 && string(arg.Sort) != "null" {
		sort, err := parseOrderedDoc(arg.Sort)
		if err != nil {
			return nil, fmt.Errorf("invalid sort: %w", err)
		}

		opts.SetSort(sort)
	}

	if len(arg.Projection) > 0 && string(arg.Projection) != "null" {
		projection, err := parseDoc(arg.Projection)
		if err != nil {
			return nil, fmt.Errorf("invalid projection: %w", err)
		}

		opts.SetProjection(projection)
	}

	return opts, nil
}

func handleVersion() *C.char {
	return okJSON(`{"name":"tiny-mongo","version":"0.2.0","driver":"go.mongodb.org/mongo-driver/v2","extendedJson":true}`)
}

func handleConnect(arg MongoArgs) *C.char {
	if arg.URI == "" {
		return errorJSON("ValidationError", "missing MongoDB URI")
	}

	clientOptions := options.Client().ApplyURI(arg.URI)

	if arg.AppName != "" {
		clientOptions.SetAppName(arg.AppName)
	}

	if arg.MaxPoolSize > 0 {
		clientOptions.SetMaxPoolSize(arg.MaxPoolSize)
	}

	if arg.MinPoolSize > 0 {
		clientOptions.SetMinPoolSize(arg.MinPoolSize)
	}

	if arg.ServerSelectionTimeoutMS > 0 {
		clientOptions.SetServerSelectionTimeout(time.Duration(arg.ServerSelectionTimeoutMS) * time.Millisecond)
	}

	if arg.ConnectTimeoutMS > 0 {
		clientOptions.SetConnectTimeout(time.Duration(arg.ConnectTimeoutMS) * time.Millisecond)
	}

	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return errorJSON("MongoConnectError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	err = client.Ping(ctx, nil)
	if err != nil {
		_ = client.Disconnect(ctx)
		return errorJSON("MongoPingError", err.Error())
	}

	id := saveClient(client)

	return okJSON(fmt.Sprintf(`{"client":%d}`, id))
}

func handleDisconnect(arg MongoArgs) *C.char {
	client, err := getClient(arg.Client)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	err = client.Disconnect(ctx)
	if err != nil {
		return errorJSON("MongoDisconnectError", err.Error())
	}

	deleteClient(arg.Client)

	return okJSON(`{"success":true}`)
}

func handleCloseAll(arg MongoArgs) *C.char {
	all := snapshotClients()

	for id, client := range all {
		ctx, cancel := makeContext(arg.TimeoutMS)
		_ = client.Disconnect(ctx)
		cancel()

		deleteClient(id)
	}

	clearClients()

	return okJSON(`{"success":true}`)
}

func handlePing(arg MongoArgs) *C.char {
	client, err := getClient(arg.Client)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	err = client.Ping(ctx, nil)
	if err != nil {
		return errorJSON("MongoPingError", err.Error())
	}

	return okJSON(`{"success":true}`)
}

func handleInsertOne(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	doc, err := parseDoc(arg.Document)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	result, err := coll.InsertOne(ctx, doc)
	if err != nil {
		return errorJSON("MongoInsertError", err.Error())
	}

	insertedID, err := jsonValue(result.InsertedID)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	return okJSON(`{"insertedId":` + insertedID + `}`)
}

func handleInsertMany(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	if len(arg.Documents) == 0 {
		return errorJSON("ValidationError", "documents cannot be empty")
	}

	docs, err := parseDocs(arg.Documents)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	result, err := coll.InsertMany(ctx, docs)
	if err != nil {
		return errorJSON("MongoInsertError", err.Error())
	}

	ids, err := jsonValue(result.InsertedIDs)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	return okJSON(`{"insertedIds":` + ids + `}`)
}

func handleFindOne(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	opts, err := applyFindOneOptions(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	var doc bson.M
	err = coll.FindOne(ctx, filter, opts).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return okJSON(`{"doc":null}`)
	}
	if err != nil {
		return errorJSON("MongoFindError", err.Error())
	}

	docJSON, err := jsonDoc(doc)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	return okJSON(`{"doc":` + docJSON + `}`)
}

func handleFindMany(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	opts, err := applyFindOptions(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return errorJSON("MongoFindError", err.Error())
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	err = cursor.All(ctx, &docs)
	if err != nil {
		return errorJSON("MongoCursorError", err.Error())
	}

	docsJSON, err := jsonValue(docs)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	return okJSON(`{"docs":` + docsJSON + `}`)
}

func handleUpdateOne(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	update, err := parseDoc(arg.Update)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	opts := options.UpdateOne()
	if arg.Upsert {
		opts.SetUpsert(true)
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	result, err := coll.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return errorJSON("MongoUpdateError", err.Error())
	}

	upsertedID, err := jsonValue(result.UpsertedID)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	out := fmt.Sprintf(
		`{"matched":%d,"modified":%d,"upsertedId":%s}`,
		result.MatchedCount,
		result.ModifiedCount,
		upsertedID,
	)

	return okJSON(out)
}

func handleUpdateMany(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	update, err := parseDoc(arg.Update)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	opts := options.UpdateMany()
	if arg.Upsert {
		opts.SetUpsert(true)
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	result, err := coll.UpdateMany(ctx, filter, update, opts)
	if err != nil {
		return errorJSON("MongoUpdateError", err.Error())
	}

	upsertedID, err := jsonValue(result.UpsertedID)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	out := fmt.Sprintf(
		`{"matched":%d,"modified":%d,"upsertedId":%s}`,
		result.MatchedCount,
		result.ModifiedCount,
		upsertedID,
	)

	return okJSON(out)
}

func handleDeleteOne(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	result, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return errorJSON("MongoDeleteError", err.Error())
	}

	return okJSON(fmt.Sprintf(`{"deleted":%d}`, result.DeletedCount))
}

func handleDeleteMany(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	result, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return errorJSON("MongoDeleteError", err.Error())
	}

	return okJSON(fmt.Sprintf(`{"deleted":%d}`, result.DeletedCount))
}

func handleCountDocuments(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	filter, err := parseDoc(arg.Filter)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	count, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return errorJSON("MongoCountError", err.Error())
	}

	return okJSON(fmt.Sprintf(`{"count":%d}`, count))
}

func handleCreateIndex(arg MongoArgs) *C.char {
	coll, err := collectionFromArg(arg)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	if len(arg.Keys) == 0 || string(arg.Keys) == "null" {
		return errorJSON("ValidationError", "missing index keys")
	}

	keys, err := parseOrderedDoc(arg.Keys)
	if err != nil {
		return errorJSON("BSONError", err.Error())
	}

	indexOptions := options.Index()

	if arg.Name != "" {
		indexOptions.SetName(arg.Name)
	}

	if arg.Unique {
		indexOptions.SetUnique(true)
	}

	model := mongo.IndexModel{
		Keys:    keys,
		Options: indexOptions,
	}

	ctx, cancel := makeContext(arg.TimeoutMS)
	defer cancel()

	name, err := coll.Indexes().CreateOne(ctx, model)
	if err != nil {
		return errorJSON("MongoIndexError", err.Error())
	}

	out, _ := json.Marshal(map[string]any{
		"name": name,
	})

	return okJSON(string(out))
}

func dispatch(method string, arg MongoArgs) *C.char {
	switch method {
	case "connect":
		return handleConnect(arg)
	case "disconnect":
		return handleDisconnect(arg)
	case "closeAll":
		return handleCloseAll(arg)
	case "ping":
		return handlePing(arg)
	case "insertOne":
		return handleInsertOne(arg)
	case "insertMany":
		return handleInsertMany(arg)
	case "findOne":
		return handleFindOne(arg)
	case "findMany":
		return handleFindMany(arg)
	case "updateOne":
		return handleUpdateOne(arg)
	case "updateMany":
		return handleUpdateMany(arg)
	case "deleteOne":
		return handleDeleteOne(arg)
	case "deleteMany":
		return handleDeleteMany(arg)
	case "countDocuments":
		return handleCountDocuments(arg)
	case "createIndex":
		return handleCreateIndex(arg)
	default:
		return errorJSON("UnknownMethod", "unknown mongo method")
	}
}

//export TinyPluginCall
func TinyPluginCall(methodC *C.char, payloadC *C.char) (ret *C.char) {
	defer func() {
		if r := recover(); r != nil {
			ret = errorJSON("Panic", fmt.Sprint(r))
		}
	}()

	method := C.GoString(methodC)
	payload := C.GoString(payloadC)

	if method == "version" {
		return handleVersion()
	}

	req, err := parseRequest(payload)
	if err != nil {
		return errorJSON("JsonError", "bad json payload")
	}

	if req.Method != "" && req.Method != method {
		return errorJSON("MethodMismatch", "method parameter does not match payload method")
	}

	arg, err := parseArg(req)
	if err != nil {
		return errorJSON("ValidationError", err.Error())
	}

	return dispatch(method, arg)
}

//export TinyPluginFree
func TinyPluginFree(ptr *C.char) {
	if ptr != nil {
		C.free(unsafe.Pointer(ptr))
	}
}
