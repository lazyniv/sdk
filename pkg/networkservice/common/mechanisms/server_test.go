// Copyright (c) 2020 Cisco and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mechanisms_test

import (
	"context"
	"io/ioutil"
	"testing"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/cls"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/memif"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/srv6"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vxlan"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
	"gonum.org/v1/gonum/stat/combin"

	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/utils/inject/injecterror"
	"github.com/networkservicemesh/sdk/pkg/networkservice/utils/null"
)

func server() networkservice.NetworkServiceServer {
	return chain.NewNetworkServiceServer(mechanisms.NewServer(map[string]networkservice.NetworkServiceServer{
		memif.MECHANISM:  null.NewServer(),
		kernel.MECHANISM: null.NewServer(),
		srv6.MECHANISM:   null.NewServer(),
		vxlan.MECHANISM:  null.NewServer(),
	}))
}

func request() *networkservice.NetworkServiceRequest {
	return &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
		MechanismPreferences: []*networkservice.Mechanism{
			{
				Cls:  cls.LOCAL,
				Type: memif.MECHANISM,
			},
			{
				Cls:  cls.LOCAL,
				Type: kernel.MECHANISM,
			},
			{
				Cls:  cls.REMOTE,
				Type: srv6.MECHANISM,
			},
			{
				Cls:  cls.REMOTE,
				Type: vxlan.MECHANISM,
			},
		},
	}
}

func permuteOverMechanismPreferenceOrder(request *networkservice.NetworkServiceRequest) []*networkservice.NetworkServiceRequest {
	var rv []*networkservice.NetworkServiceRequest
	numMechanism := len(request.GetMechanismPreferences())
	for k := numMechanism; k > 0; k-- {
		permutationGenerator := combin.NewPermutationGenerator(numMechanism, numMechanism)
		for permutationGenerator.Next() {
			permutation := permutationGenerator.Permutation(nil)
			req := request.Clone()
			req.MechanismPreferences = nil
			for _, index := range permutation {
				req.MechanismPreferences = append(req.MechanismPreferences, request.GetMechanismPreferences()[index])
			}
			rv = append(rv, req)
		}
	}
	return rv
}

func TestSelectMechanism(t *testing.T) {
	defer goleak.VerifyNone(t)
	logrus.SetOutput(ioutil.Discard)
	server := server()
	for _, request := range permuteOverMechanismPreferenceOrder(request()) {
		assert.Nil(t, request.GetConnection().GetMechanism(), "SelectMechanismContract requires request.GetConnection().GetMechanism() nil")
		assert.Greater(t, len(request.GetMechanismPreferences()), 0, "serverBasicMechanismContract requires len(request.GetMechanismPreferences()) > 0")
		conn, err := server.Request(context.Background(), request)
		assert.Nil(t, err)
		assert.NotNil(t, conn)
		assert.NotNil(t, conn.GetMechanism())
		assert.Equal(t, request.GetMechanismPreferences()[0].GetCls(), conn.GetMechanism().GetCls(), "Unexpected response to request %+v", request)
		assert.Equal(t, request.GetMechanismPreferences()[0].GetType(), conn.GetMechanism().GetType(), "Unexpected response to request %+v", request)
		_, err = server.Close(context.Background(), conn)
		assert.Nil(t, err)
	}
}

func TestDontSelectMechanismIfSet(t *testing.T) {
	defer goleak.VerifyNone(t)
	logrus.SetOutput(ioutil.Discard)
	server := server()
	for _, request := range permuteOverMechanismPreferenceOrder(request()) {
		request.Connection = &networkservice.Connection{Mechanism: request.GetMechanismPreferences()[len(request.GetMechanismPreferences())-1]}
		assert.NotNil(t, request.GetConnection().GetMechanism())
		assert.Greater(t, len(request.GetMechanismPreferences()), 0, "serverBasicMechanismContract requires len(request.GetMechanismPreferences()) > 0")
		conn, err := server.Request(context.Background(), request)
		assert.Nil(t, err)
		assert.NotNil(t, conn)
		assert.Equal(t, request.GetConnection().GetMechanism(), conn.GetMechanism())
	}
}

func TestUnsupportedMechanismPreference(t *testing.T) {
	defer goleak.VerifyNone(t)
	logrus.SetOutput(ioutil.Discard)
	request := request()
	request.MechanismPreferences = []*networkservice.Mechanism{
		{Cls: "NOT_A_CLS", Type: "NOT_A_TYPE"},
	}
	conn, err := server().Request(context.Background(), request)
	assert.Nil(t, conn)
	assert.NotNil(t, err)
	_, err = server().Close(context.Background(), &networkservice.Connection{Mechanism: &networkservice.Mechanism{Cls: "NOT_A_CLS", Type: "NOT_A_TYPE"}})
	assert.NotNil(t, err)
}

func TestUnsupportedMechanism(t *testing.T) {
	defer goleak.VerifyNone(t)
	logrus.SetOutput(ioutil.Discard)
	request := request()
	request.GetConnection().Mechanism = &networkservice.Mechanism{
		Cls:  "NOT_A_CLS",
		Type: "NOT_A_TYPE",
	}
	conn, err := server().Request(context.Background(), request)
	assert.Nil(t, conn)
	assert.NotNil(t, err)
	_, err = server().Close(context.Background(), &networkservice.Connection{Mechanism: &networkservice.Mechanism{Cls: "NOT_A_CLS", Type: "NOT_A_TYPE"}})
	assert.NotNil(t, err)
}

func TestDownstreamError(t *testing.T) {
	defer goleak.VerifyNone(t)
	logrus.SetOutput(ioutil.Discard)
	request := request()
	request.GetConnection().Mechanism = &networkservice.Mechanism{
		Cls:  cls.LOCAL,
		Type: memif.MECHANISM,
	}
	server := chain.NewNetworkServiceServer(mechanisms.NewServer(map[string]networkservice.NetworkServiceServer{
		memif.MECHANISM: injecterror.NewServer(),
	}))
	conn, err := server.Request(context.Background(), request)
	assert.Nil(t, conn)
	assert.NotNil(t, err)
	_, err = server.Close(context.Background(), &networkservice.Connection{Mechanism: &networkservice.Mechanism{Cls: "NOT_A_CLS", Type: "NOT_A_TYPE"}})
	assert.NotNil(t, err)
}
