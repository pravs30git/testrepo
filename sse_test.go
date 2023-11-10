package handlers

import (
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"unisys.com/slec/conf"
	"unisys.com/slec/filestore"
)

var myconfig conf.Config

func prepareTestEnvironment() {
	myconfig.GetConf()
	dbNames := append(myconfig.FileServer.Tenants, "config")
	err := filestore.InitializeDB(myconfig.FileServer.Dir, dbNames)
	if err != nil {
		log.Fatalln("Unable to initialize JSON DB. Reason - ", err.Error())
	}
	err = InitializeTenantDBs()
	if err != nil {
		log.Println("Error when initialize Tenant DB. Reason - ", err.Error())
	}
}

func TestGetServerSentEventHandler(t *testing.T) {
	prepareTestEnvironment()
	c, rw := getSSEhttpContext()
	GetServerSentEventHandler(c)
	if rw.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("Error: received %v expected  %v Response body String  %s",
			rw.Result().StatusCode, http.StatusInternalServerError, rw.Body.String())
	}

}

func TestBroker_handleClients(t *testing.T) {
	type args struct {
		c *gin.Context
	}
	tests := []struct {
		name   string
		broker *Broker
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.broker.handleClients(tt.args.c)
		})
	}
}

func getSSEhttpContext() (*gin.Context, *httptest.ResponseRecorder) {

	w := httptest.NewRecorder()
	ctx := getMyTestGinContext(w)
	params := []gin.Param{
		{
			Key:   "tenant",
			Value: "dummy",
		},
	}

	myMockJsonGet(ctx, params)
	return ctx, w
}
