// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package respond

import (
	"fmt"

	ir "k8s.io/idl/ckdl-ir/goir/backend"
)

// TODO: actual structured responses here

// TODO: XYZAt logs
// TODO: allow switching output format

func genMsg(lvl ir.Log_Level, loggedErr error, msg string, kvPairs ...interface{}) {
	numPairs := len(kvPairs)/2 + len(kvPairs)%2
	logPairs := make([]*ir.Log_Trace_KeyValue, numPairs)
	for i, item := range kvPairs {
		pairInd := i/2
		if i % 2 == 0 {
			logPairs[pairInd] = &ir.Log_Trace_KeyValue{Key: item.(string)}
		} else {
			// TODO: OtherNode
			logPairs[pairInd].Value = &ir.Log_Trace_KeyValue_Str{Str: fmt.Sprintf("%v", item)}
		}
	}
	line := ir.Log{
		Lvl: lvl,
		Trace: []*ir.Log_Trace{{Message: msg, Values: logPairs}},
	}
	if loggedErr != nil {
		line.Trace = append(line.Trace, &ir.Log_Trace{Message: loggedErr.Error()})
	}

	Write(&ir.Response{Type: &ir.Response_Log{Log: &line}})
	// panic last so we get the log out
	if len(kvPairs) % 2 != 0 {
		panic("uneven number of key-value items specified in log line")
	}
}

func GeneralInfo(msg string, kvPairs ...interface{}) {
	genMsg(ir.Log_INFO, nil, msg, kvPairs...)
}

func GeneralError(err error, msg string, kvPairs ...interface{}) {
	genMsg(ir.Log_ERROR, err, msg, kvPairs...)
}
