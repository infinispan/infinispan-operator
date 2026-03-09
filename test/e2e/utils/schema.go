package utils

import (
	"fmt"

	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"k8s.io/apimachinery/pkg/util/wait"
)

type SchemaHelper struct {
	Client       HTTPClient
	SchemaName   string
	SchemaClient api.ProtoSchema
}

func NewSchemaHelper(schemaName string, client HTTPClient) *SchemaHelper {
	return &SchemaHelper{
		SchemaClient: ispnClient.New(CurrentOperand, client).Schema(schemaName),
		SchemaName:   schemaName,
		Client:       client,
	}
}

func (s *SchemaHelper) Create(schema string) {
	_, err := s.SchemaClient.Create(schema)
	ExpectNoError(err)
}

func (s *SchemaHelper) CreateOrUpdate(schema string) {
	_, err := s.SchemaClient.CreateOrUpdate(schema)
	ExpectNoError(err)
}

func (s *SchemaHelper) Delete() {
	ExpectNoError(s.SchemaClient.Delete())
}

func (s *SchemaHelper) Get() string {
	schema, err := s.SchemaClient.Get()
	ExpectNoError(err)
	return schema
}

func (s *SchemaHelper) AssertSchemaExists() {
	schema := s.Get()
	if schema == "" {
		panic(fmt.Sprintf("Schema %s does not exist", s.SchemaName))
	}
}

func (s *SchemaHelper) WaitForSchemaToExist() {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		schema, err := s.SchemaClient.Get()
		if err != nil {
			return false, err
		}
		return schema != "", nil
	})
	ExpectNoError(err)
}

func (s *SchemaHelper) WaitForSchemaToNotExist() {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		schema, err := s.SchemaClient.Get()
		if err != nil {
			return false, err
		}
		return schema == "", nil
	})
	ExpectNoError(err)
}
