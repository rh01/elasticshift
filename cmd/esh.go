package main

import (
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"net/http"

	"context"
	"fmt"

	"github.com/spf13/viper"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"

	"os"

	"time"

	"strconv"

	"github.com/Sirupsen/logrus"
	"gitlab.com/conspico/esh"
	"gopkg.in/mgo.v2"
)

func main() {

	ctx := esh.AppContext{}

	// Unwrap DEK - data encryption key
	ctx.Context = context.Background()

	// Logger
	logger := logrus.New()
	/*f, err := os.OpenFile("esh.log", os.O_WRONLY|os.O_CREATE, 777)
	if err != nil {
		logger.Fatalln("Unable to create log file.", err)
	}
	logrus.SetOutput(f) */
	ctx.Logger = logger

	logger.Infoln("Starting ESH Server...")
	// CLI args

	// App configuration
	logger.Infoln("Reading properties...")
	vip := viper.New()
	vip.SetConfigType("yml")
	vip.SetConfigFile("esh.yml")
	vip.ReadInConfig()

	config := esh.Config{}
	vip.Unmarshal(&config)

	ctx.Config = config

	retryDuration, _ := time.ParseDuration(strconv.Itoa(config.DB.Retry) + "s")
	timeoutDuration, _ := time.ParseDuration(strconv.Itoa(config.DB.Timeout) + "s")

	// DB Initialization
	var session *mgo.Session
	var err error
	tryit := true
	for tryit {

		logger.Infoln("Connecting to database...")
		session, err = mgo.DialWithInfo(&mgo.DialInfo{
			Addrs:    []string{config.DB.Server},
			Username: config.DB.Username,
			Password: config.DB.Password,
			Database: config.DB.Name,
			Timeout:  timeoutDuration,
		})
		if err != nil {

			logger.Errorln(fmt.Sprintf("Connecting database failed, retrying in %d seconds.[", config.DB.Retry), err, "]")
			time.Sleep(retryDuration)

		} else {

			// Ping function checks the database connectivity
			session.Refresh()
			dberr := session.Ping()
			if dberr != nil {
				logger.Errorln(fmt.Sprintf("Ping DB failed, retrying in %d seconds", config.DB.Retry), err)
			} else {
				logger.Infoln("Database connected successfully")
				tryit = false
			}
		}
	}

	// set the configurations
	session.SetMode(mgo.Monotonic, config.DB.Monotonic)

	// starting a background process to reconeb;nct db in case failure.
	go reconnectOnFailure(ctx, session)
	defer session.Close()

	// TLS

	// Init datastore
	ds := esh.NewDatasource(config.DB.Name, session)
	ctx.Datasource = ds

	ctx.TeamDatastore = esh.NewTeamDatastore(ds)
	ctx.UserDatastore = esh.NewUserDatastore(ds)
	ctx.RepoDatastore = esh.NewRepoDatastore(ds)
	ctx.SysconfDatastore = esh.NewSysconfDatastore(ds)

	// load keys
	signer, err := loadKey(config.Key.Signer)
	if err != nil {
		logger.Fatalln("Cannot load signer key", err)
		os.Exit(-1)
	}
	ctx.Signer = signer

	verifier, err := loadKey(config.Key.Verifier)
	if err != nil {
		logger.Fatalln("Cannot load verifier key", err)
		os.Exit(-1)
	}
	ctx.Verifier = verifier

	// Initialize services
	ctx.TeamService = esh.NewTeamService(ctx)
	ctx.UserService = esh.NewUserService(ctx)
	ctx.VCSService = esh.NewVCSService(ctx)
	ctx.RepoService = esh.NewRepoService(ctx)

	router := mux.NewRouter()
	ctx.Router = router

	// Router (includes subdomain)
	esh.MakeHandlers(ctx)

	// ESH UI pages
	csrf.Secure(config.CSRF.Secure)
	csrfHandler := csrf.Protect([]byte(config.CSRF.Key))
	router.PathPrefix("/").Handler(ctx.PublicChain.Then(http.FileServer(http.Dir("./dist/"))))
	router.Handle("/", ctx.PublicChain.Then(csrfHandler(router)))

	// Start the server
	logger.Info("Elasticshift Server started successfully.")

	go http.ListenAndServeTLS(":443", config.Key.Certfile, config.Key.Keyfile, router)
	logger.Infoln(http.ListenAndServe(":80", http.HandlerFunc(redirect)))
	//logger.Infoln(http.ListenAndServe(":5050", router))
}

func loadKey(path string) (interface{}, error) {

	keyBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyBlock, _ := pem.Decode(keyBytes)

	switch keyBlock.Type {
	case "PUBLIC KEY":
		return x509.ParsePKIXPublicKey(keyBlock.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type %q", keyBlock.Type)
	}
}

func reconnectOnFailure(ctx esh.AppContext, session *mgo.Session) {

	reconnectDuration, _ := time.ParseDuration(strconv.Itoa(ctx.Config.DB.Reconnect) + "s")

	disconnected := false
	for {

		time.Sleep(reconnectDuration)

		session.Refresh()

		// Ping function checks the database connectivity
		err := session.Ping()
		if err != nil {
			disconnected = true
			ctx.Logger.Errorln(fmt.Sprintf("DB ping failed, something went wrong. Reconnecting in %d seconds", ctx.Config.DB.Reconnect))
		} else if disconnected {
			ctx.Logger.Infoln("Reconnected to database successfully.")
			disconnected = false
		}
	}
}

func redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.String(), http.StatusMovedPermanently)
}