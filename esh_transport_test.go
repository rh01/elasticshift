// Package esh
// Author Ghazni Nattarshah
// Date: Jan 3, 2017
package esh

import (
	"testing"
	"github.com/stretchr/testify/suite"
	"gopkg.in/mgo.v2"
	"github.com/spf13/viper"
	"github.com/Sirupsen/logrus"
	"gitlab.com/conspico/esh/core/util"
	"context"
	"github.com/gorilla/mux"
)

func TestTransport(t *testing.T) {
	suite.Run(t, new(TransportTestSuite))
}

type TransportTestSuite struct {
	suite.Suite
	config Config
	session *mgo.Session
	svc TeamService
	appCtx AppContext

	//test specific
	team string
}

func (suite *TransportTestSuite) SetupTest() {

	vip := viper.New()
	vip.SetConfigType("yml")
	vip.SetConfigFile("esh.yml")
	vip.ReadInConfig()

	config := Config{}
	vip.Unmarshal(&config)

	appCtx := AppContext{}
	appCtx.Context = context.Background()
	appCtx.Config = config
	appCtx.Signer, _  = util.LoadKey(config.Key.Signer)
	appCtx.Verifier, _  = util.LoadKey(config.Key.Verifier)
	appCtx.Logger = logrus.New()
	appCtx.Router = mux.NewRouter()

	suite.appCtx = appCtx
}

func (suite *TransportTestSuite) TearDownTest() {
	suite.appCtx.Context.Done()
}

func (suite *TransportTestSuite) TestMakeHandlers() {
	MakeHandlers(suite.appCtx)
}