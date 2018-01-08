/*
Copyright 2017 The Elasticshift Authors.
*/
package store

import (
	"encoding/json"

	"gitlab.com/conspico/elasticshift/core/utils"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var (
	recordNotFound = "record not found"
)

// Store ..
// Abstract database interactions.
type Core interface {
	Execute(handleFunc func(c *mgo.Collection))
	Save(model interface{}) error
	Upsert(selector interface{}, model interface{}) (*mgo.ChangeInfo, error)
	FindAll(query interface{}, model interface{}) error
	FindOne(query interface{}, model interface{}) error
	FindByID(id string, model interface{}) error
	FindByObjectID(id bson.ObjectId, model interface{}) error
	Exist(selector interface{}) (bool, error)
	Remove(id interface{}) error
	RemoveMultiple(ids []interface{}) error
	GetSession() *mgo.Session
}

type Database struct {
	session *mgo.Session
	Name    string
}

// New ..
// Create a new base datasource
func NewDatabase(dbname string, session *mgo.Session) Database {
	return Database{Name: dbname, session: session}
}

// Store ..
// A base datasource that performs actualy sql interactions.
type Store struct {
	Database       Database
	CollectionName string
}

func (s *Store) GetSession() *mgo.Session {
	return s.Database.session
}

// Execute given func with a active session against the database
func (s *Store) Execute(handle func(c *mgo.Collection)) {

	ses := s.Database.session.Copy()
	defer ses.Close()

	handle(ses.DB(s.Database.Name).C(s.CollectionName))
	return
}

// Checks whether the given document exist in a collection
func (s *Store) Exist(selector interface{}) (bool, error) {

	var count int
	var err error
	s.Execute(func(c *mgo.Collection) {
		count, err = c.Find(selector).Count()
	})

	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Insert operation for a model on a collection
func (s *Store) Save(model interface{}) error {

	var err error
	s.Execute(func(c *mgo.Collection) {
		err = c.Insert(model)
	})
	return err
}

// Upsert for a model on a collection, based on a selector
func (s *Store) Upsert(selector interface{}, model interface{}) (*mgo.ChangeInfo, error) {

	var info *mgo.ChangeInfo
	var err error
	s.Execute(func(c *mgo.Collection) {
		info, err = c.Upsert(selector, model)
	})
	return info, err
}

// FindAll the document matches the query on a collection.
func (s *Store) FindAll(query interface{}, model interface{}) error {

	var err error
	s.Execute(func(c *mgo.Collection) {
		err = c.Find(query).All(model)
	})
	return err
}

// FindOne document matches the query on a collection
func (s *Store) FindOne(query interface{}, model interface{}) error {

	var err error
	s.Execute(func(c *mgo.Collection) {
		err = c.Find(query).One(model)
	})
	return err
}

// FindByID matches the document by _id
func (s *Store) FindByID(id string, model interface{}) error {
	return s.FindByObjectID(bson.ObjectIdHex(id), model)
}

// FindByID matches the document by _id
func (s *Store) FindByObjectID(id bson.ObjectId, model interface{}) error {
	return s.FindOne(bson.M{"_id": id}, model)
}

// Remove a document based on id
func (s *Store) Remove(id interface{}) error {

	var err error
	s.Execute(func(c *mgo.Collection) {
		err = c.RemoveId(id)
	})
	return err
}

// RemoveMultiple document based on gived ids
func (s *Store) RemoveMultiple(ids []interface{}) error {

	var err error
	s.Execute(func(c *mgo.Collection) {
		err = c.Remove(bson.M{"_id": bson.M{"$in": ids}})
	})
	return err
}

func encode(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

func decode(data []byte, out interface{}) error {
	return json.Unmarshal(data, out)
}

// NewID ..
// Creates a new UUID and returns string
func NewID() string {
	return utils.NewUUID()
}
