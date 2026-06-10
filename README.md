# TinyMongoDB

MongoDB support for Tiny, powered by the official MongoDB Go driver through Tiny’s native plugin system.

TinyMongoDB gives Tiny apps a clean MongoDB API while keeping the hard database-driver work inside a native Go plugin.

```js
import std "io"
import lib "confh/TinyMongoDB" as mongo

const client = mongo.connect()
const users = client.database("tiny_demo").collection("users")

users.insertOne({
    name: "confis",
    age: 18,
    createdAt: mongo.date("2026-06-10T12:00:00Z")
})

const user = users.findOne({
    name: "confis"
})

io.println(user.createdAt.date)

client.disconnect()
```

## Features

* Connect to local MongoDB or MongoDB Atlas
* Uses MongoDB’s official Go driver internally
* Supports authentication, TLS, pooling, and BSON through the Go driver
* Tiny-friendly API for:

  * `connect`
  * `ping`
  * `insertOne`
  * `insertMany`
  * `findOne`
  * `findMany`
  * `updateOne`
  * `updateMany`
  * `deleteOne`
  * `deleteMany`
  * `countDocuments`
  * `createIndex`
  * `closeAll`
* MongoDB helper values:

  * `mongo.id(...)`
  * `mongo.objectId(...)`
  * `mongo.date(...)`
  * `mongo.decimal(...)`
  * `mongo.regex(...)`
  * `mongo.binary(...)`
* Update helpers:

  * `mongo.set(...)`
  * `mongo.unset(...)`
  * `mongo.inc(...)`
  * `mongo.push(...)`
* `.env` support through the dotenv package
* Safe default `findMany` limit
* Tiny-friendly returned documents

## Native plugin

TinyMongoDB requires a native plugin file.

Expected plugin names:

```text
Windows: plugins/tiny_mongo.dll
Linux:   plugins/tiny_mongo.so
macOS:   plugins/tiny_mongo.dylib
```

The Tiny wrapper talks to the plugin using Tiny’s simple plugin ABI:

```text
method: string
payload: JSON string
return: JSON string
```

The plugin uses the Go MongoDB driver internally.

## Installation

Clone or install the package into your Tiny project.

Example local structure:

```text
my-app/
  main.tiny
  .env
  libs/
    mongodb.tiny
  plugins/
    tiny_mongo.dll
```

Import it:

```js
import "./libs/mongodb.tiny" as mongo
```

Or, if installed through Tiny’s package manager:

```js
import "mongodb" as mongo
```

## Configuration

TinyMongoDB can connect using an explicit URI:

```js
const client = mongo.connect("mongodb://localhost:27017")
```

Or it can read from `.env`:

```env
MONGODB_URL=mongodb://localhost:27017
```

Then:

```js
const client = mongo.connect()
```

Lookup order:

```text
MONGODB_URL
MONGODB_URI
MONGO_URL
```

Never hardcode real MongoDB passwords in source code. Put credentials in `.env`.

## Quick start

```js
import std "io"
import "mongodb" as mongo

io.println("TinyMongoDB example")

const client = mongo.connect()
client.ping()

const users = client.database("tiny_demo").collection("users")

users.createIndex({ email: 1 }, "email_unique", true)

users.insertOne({
    name: "confis",
    email: "confis@example.com",
    age: 18,
    createdAt: mongo.date("2026-06-10T12:00:00Z")
})

const found = users.findOne({
    email: "confis@example.com"
})

io.println(found.name)
io.println(found.createdAt.date)

users.updateOne(
    { email: "confis@example.com" },
    mongo.set({
        age: 19
    })
)

const adults = users.findMany(
    { age: { "$gte": 18 } },
    100,
    0,
    { age: -1 },
    { name: 1, email: 1, age: 1, createdAt: 1 }
)

io.println(adults)

const count = users.countDocuments({})
io.println(count)

client.disconnect()
```

## Using ObjectIDs

When sending an ObjectID to MongoDB, use `mongo.id(...)`:

```js
const user = users.findOne({
    "_id": mongo.id("665f1234567890abcdef1234")
})
```

Returned ObjectIDs are Tiny-friendly:

```js
io.println(user._id.id)
```

Not:

```js
user._id["$oid"]
```

TinyMongoDB normalizes MongoDB Extended JSON output before returning documents to Tiny.

## Using dates

When sending a MongoDB date:

```js
users.insertOne({
    name: "Tiny",
    createdAt: mongo.date("2026-06-10T12:00:00Z")
})
```

Returned dates are Tiny-friendly:

```js
io.println(user.createdAt.date)
```

Not:

```js
user.createdAt["$date"]
```

## API

### `mongo.connect(uri = "", timeoutMs = 10000, appName = "tiny-mongo", maxPoolSize = 0, minPoolSize = 0, serverSelectionTimeoutMs = 0, connectTimeoutMs = 0)`

Connects to MongoDB and returns a `MongoClient`.

```js
const client = mongo.connect("mongodb://localhost:27017")
```

With `.env`:

```js
const client = mongo.connect()
```

### `client.ping(timeoutMs = 10000)`

Checks whether MongoDB is reachable.

```js
client.ping()
```

### `client.database(name)`

Returns a database wrapper.

```js
const db = client.database("tiny_demo")
```

### `db.collection(name)`

Returns a collection wrapper.

```js
const users = db.collection("users")
```

### `collection.insertOne(document, timeoutMs = 10000)`

Inserts one document.

```js
users.insertOne({
    name: "confis",
    age: 18
})
```

### `collection.insertMany(documents, timeoutMs = 10000)`

Inserts multiple documents.

```js
users.insertMany([
    { name: "Tiny" },
    { name: "Mongo" }
])
```

### `collection.findOne(filter = {}, sort = null, projection = null, timeoutMs = 10000)`

Finds one document.

```js
const user = users.findOne({
    email: "confis@example.com"
})
```

Returns `null` if no document is found.

### `collection.findMany(filter = {}, limit = 100, skip = 0, sort = null, projection = null, timeoutMs = 10000)`

Finds many documents.

```js
const users = users.findMany(
    { age: { "$gte": 18 } },
    100,
    0,
    { age: -1 },
    { name: 1, age: 1 }
)
```

Safety defaults:

```text
Default limit: 100
Maximum plugin limit: 1000
```

### `collection.updateOne(filter, update, upsert = false, timeoutMs = 10000)`

Updates one document.

```js
users.updateOne(
    { email: "confis@example.com" },
    mongo.set({ age: 19 })
)
```

### `collection.updateMany(filter, update, upsert = false, timeoutMs = 10000)`

Updates multiple documents.

```js
users.updateMany(
    { active: true },
    mongo.inc({ loginCount: 1 })
)
```

### `collection.deleteOne(filter, timeoutMs = 10000)`

Deletes one document.

```js
users.deleteOne({
    email: "tiny@example.com"
})
```

### `collection.deleteMany(filter, timeoutMs = 10000)`

Deletes multiple documents.

```js
users.deleteMany({
    active: false
})
```

### `collection.countDocuments(filter = {}, timeoutMs = 10000)`

Counts matching documents.

```js
const count = users.countDocuments({})
```

### `collection.createIndex(keys, name = "", unique = false, timeoutMs = 10000)`

Creates an index.

```js
users.createIndex({ email: 1 }, "email_unique", true)
```

### `client.disconnect(timeoutMs = 10000)`

Disconnects the client.

```js
client.disconnect()
```

### `mongo.closeAll(timeoutMs = 10000)`

Disconnects every client held by the native plugin.

```js
mongo.closeAll()
```

## Helper values

### `mongo.id(value)`

Creates a MongoDB ObjectID input value.

```js
mongo.id("665f1234567890abcdef1234")
```

### `mongo.objectId(value)`

Alias for `mongo.id`.

```js
mongo.objectId("665f1234567890abcdef1234")
```

### `mongo.date(value)`

Creates a MongoDB date input value.

```js
mongo.date("2026-06-10T12:00:00Z")
```

### `mongo.decimal(value)`

Creates a Decimal128 input value.

```js
mongo.decimal("12.50")
```

### `mongo.regex(pattern, options = "")`

Creates a MongoDB regular expression input value.

```js
mongo.regex("^tiny", "i")
```

### `mongo.binary(base64, subType = "00")`

Creates a MongoDB binary input value.

```js
mongo.binary("SGVsbG8=", "00")
```

## Update helpers

### `mongo.set(values)`

```js
mongo.set({
    age: 19
})
```

Creates:

```json
{ "$set": { "age": 19 } }
```

### `mongo.unset(values)`

```js
mongo.unset({
    oldField: ""
})
```

### `mongo.inc(values)`

```js
mongo.inc({
    loginCount: 1
})
```

### `mongo.push(values)`

```js
mongo.push({
    tags: "tiny"
})
```

## Building the native plugin

The plugin is written in Go and uses `c-shared` mode.

Install dependencies:

```bash
go mod init tiny-mongo-plugin
go get go.mongodb.org/mongo-driver/v2/mongo
go mod tidy
```

Build for Windows:

```powershell
go build -buildmode=c-shared -ldflags="-s -w" -o tiny_mongo.dll mongodb.go
```

Build for Linux:

```bash
go build -buildmode=c-shared -ldflags="-s -w" -o tiny_mongo.so mongodb.go
```

Build for macOS:

```bash
go build -buildmode=c-shared -ldflags="-s -w" -o tiny_mongo.dylib mongodb.go
```

This also creates a `.h` file. Tiny only needs the shared library file.

## Security notes

Do not commit `.env` files containing real credentials.

Bad:

```js
const client = mongo.connect("mongodb://admin:password@example.com:27017")
```

Good:

```env
MONGODB_URL=mongodb://admin:password@example.com:27017
```

```js
const client = mongo.connect()
```

Also make sure your MongoDB server is not publicly exposed without proper authentication, firewall rules, and TLS.

## Status

TinyMongoDB is intended to be a serious native-backed MongoDB package for Tiny.

Recommended before calling a release stable:

* Test local MongoDB
* Test MongoDB Atlas
* Test bad URI errors
* Test invalid credentials
* Test ObjectID filters
* Test date fields
* Test large `findMany` results
* Test parallel Tiny requests
* Test Windows, Linux, and macOS plugin loading

## Why native?

MongoDB is not just JSON over HTTP. A real driver needs BSON support, authentication, TLS, pooling, server selection, cursors, timeouts, and many protocol details.

TinyMongoDB keeps the Tiny API simple while letting the official Go driver handle the heavy machinery.

## License

MIT
