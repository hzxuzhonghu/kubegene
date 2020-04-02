/*
Copyright 2018 The Kubegene Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"fmt"
	"time"

	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	genev1alpha1 "kubegene.io/kubegene/pkg/apis/gene/v1alpha1"
	"kubegene.io/kubegene/pkg/graph"
	"strconv"
)

var keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

func GetJobCondition(j *batch.Job) (batch.JobConditionType, string) {
	for _, c := range j.Status.Conditions {
		if c.Type == batch.JobComplete && c.Status == v1.ConditionTrue {
			return batch.JobComplete, c.Message
		} else if c.Type == batch.JobFailed && c.Status == v1.ConditionTrue {
			return batch.JobFailed, c.Message
		}
	}

	return "", ""
}

func IsJobFinished(j *batch.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batch.JobComplete || c.Type == batch.JobFailed) && c.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func KeyOf(obj interface{}) string {
	key, err := keyFunc(obj)
	if err != nil {
		panic("can not get key for obj")
	}
	return key
}

func IsExecutionCompleted(exec *genev1alpha1.Execution) bool {
	switch exec.Status.Phase {
	case genev1alpha1.VertexSucceeded, genev1alpha1.VertexError, genev1alpha1.VertexFailed:
		return true
	default:
		return false
	}
}

func InitializeVertexStatus(vertexName string,
	phase genev1alpha1.VertexPhase,
	message string,
	children []*graph.Vertex) genev1alpha1.VertexStatus {

	glog.V(2).Infof("initial %s status, phase: %s", vertexName, phase)

	childStr := make([]string, 0)
	for _, child := range children {
		childStr = append(childStr, VertexId(child.Data.Job.Name))
	}

	vertexId := VertexId(vertexName)
	vertexStatus := genev1alpha1.VertexStatus{
		ID:        vertexId,
		Name:      vertexName,
		Phase:     phase,
		Message:   message,
		StartedAt: metav1.Now(),
		Children:  childStr,
		Type:      genev1alpha1.JobVertexType, // TODO: set this filed according to real scenario
	}

	return vertexStatus
}

func VertexId(vertexName string) string {
	return vertexName
	//hash := fnv.New32a()
	//hash.Write([]byte(vertexName))
	//return fmt.Sprintf("%s", hash.Sum32())
}

func MarkExecutionSuccess(exec *genev1alpha1.Execution, message string) {
	MarkExecutionPhase(exec, genev1alpha1.VertexSucceeded, message)
}

func MarkExecutionFailed(exec *genev1alpha1.Execution, message string) {
	MarkExecutionPhase(exec, genev1alpha1.VertexFailed, message)
}

func MarkExecutionError(exec *genev1alpha1.Execution, err error) {
	MarkExecutionPhase(exec, genev1alpha1.VertexError, err.Error())
}

func MarkExecutionRunning(exec *genev1alpha1.Execution, message string) {
	MarkExecutionPhase(exec, genev1alpha1.VertexRunning, message)
}

func MarkExecutionPhase(exec *genev1alpha1.Execution, phase genev1alpha1.VertexPhase, message string) {
	if exec.Status.Phase != phase {
		glog.V(4).Infof("execution %s phase %s -> %s", KeyOf(exec), exec.Status.Phase, phase)
		exec.Status.Phase = phase
	}

	// update start time if it is zero
	if exec.Status.StartedAt.IsZero() {
		exec.Status.StartedAt = metav1.Time{Time: time.Now().UTC()}
	}

	if exec.Status.Message != message {
		exec.Status.Message = message
	}

	if IsExecutionCompleted(exec) {
		if exec.Status.FinishedAt.IsZero() {
			exec.Status.FinishedAt = metav1.Time{Time: time.Now().UTC()}
		}
	}

	if exec.Status.Vertices == nil {
		exec.Status.Vertices = make(map[string]genev1alpha1.VertexStatus)
	}

}

func MarkVertexSuccess(exec *genev1alpha1.Execution, vertexName string, message string) {
	MarkVertexPhase(exec, vertexName, genev1alpha1.VertexSucceeded, message)
}

func MarkVertexFailed(exec *genev1alpha1.Execution, vertexName string, message string) {
	MarkVertexPhase(exec, vertexName, genev1alpha1.VertexFailed, message)
}

func MarkVertexError(exec *genev1alpha1.Execution, vertexName string, err error) {
	MarkVertexPhase(exec, vertexName, genev1alpha1.VertexError, err.Error())
}

func GetVertexStatus(exec *genev1alpha1.Execution, vertexName string) *genev1alpha1.VertexStatus {
	vertexId := VertexId(vertexName)
	vertexStatus, ok := exec.Status.Vertices[vertexId]
	if !ok {
		return nil
	}

	return &vertexStatus
}

func MarkVertexPhase(exec *genev1alpha1.Execution, vertexName string, phase genev1alpha1.VertexPhase, message string) {
	vertexStatus := GetVertexStatus(exec, vertexName)
	if vertexStatus == nil {
		panic(fmt.Sprintf("can not get status fot vertex %s", vertexName))
	}

	if vertexStatus.Phase != phase {
		glog.V(4).Infof("vertex %s phase %s -> %s", vertexStatus.Name, vertexStatus.Phase, phase)
		vertexStatus.Phase = phase
	}

	if vertexStatus.Message != message {
		vertexStatus.Message = message
	}

	if vertexStatus.Phase == genev1alpha1.VertexSucceeded && vertexStatus.FinishedAt.IsZero() {
		vertexStatus.FinishedAt = metav1.Now()
	}

	exec.Status.Vertices[vertexStatus.ID] = *vertexStatus
}

// RuleSatisfied returns true if the MatchRule matches the input kv.
// There is a match in the following cases:
// (1) The operator is Exists and Labels has the MatchRule's key.
// (2) The operator is In, Labels has the MatchRule's key and map'
//     value for that key is in MatchRule's value set.
// (3) The operator is NotIn, map has the MatchRule's key and
//     map kv' value for that key is not in MatchRule's value set.
// (4) The operator is DoesNotExist or NotIn and map kv does not have the
//     MatchRule's key.
// (5) The operator is GreaterThanOperator or LessThanOperator, and map kv has
//     the MatchRule's key and the corresponding value satisfies mathematical inequality.

func RuleSatisfied(r genev1alpha1.MatchRule, kv map[string]string) bool {

	switch r.Operator {
	case genev1alpha1.MatchOperatorOpIn, genev1alpha1.MatchOperatorOpEqual, genev1alpha1.MatchOperatorOpDoubleEqual:
		val, ok := kv[r.Key]
		if !ok {
			return false
		}
		return hasValue(r, val)
	case genev1alpha1.MatchOperatorOpNotIn, genev1alpha1.MatchOperatorOpNotEqual:
		val, ok := kv[r.Key]
		if !ok {
			return true
		}
		return !hasValue(r, val)
	case genev1alpha1.MatchOperatorOpExists:
		_, ok := kv[r.Key]
		return ok
	case genev1alpha1.MatchOperatorOpDoesNotExist:
		_, ok := kv[r.Key]
		return !ok
	case genev1alpha1.MatchOperatorOpGt, genev1alpha1.MatchOperatorOpLt:
		val, ok := kv[r.Key]
		if !ok {
			return false
		}
		lsValue, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			glog.V(2).Infof("ParseInt failed for value %+v in key &val %+v, %+v", val, kv, err)
			return false
		}

		// There should be only one strValue in r.Values, and can be converted to a integer.
		if len(r.Values) != 1 {
			glog.V(2).Infof("Invalid values count %+v of match rule %#v, for 'Gt', 'Lt' operators, exactly one value is required", len(r.Values), r)
			return false
		}

		var rValue int64
		for i := range r.Values {
			rValue, err = strconv.ParseInt(r.Values[i], 10, 64)
			if err != nil {
				glog.V(2).Infof("ParseInt failed for value %+v in matchrule %#v, for 'Gt', 'Lt' operators, the value must be an integer", r.Values[i], r)
				return false
			}
		}
		return (r.Operator == genev1alpha1.MatchOperatorOpGt && lsValue > rValue) || (r.Operator == genev1alpha1.MatchOperatorOpLt && lsValue < rValue)
	default:
		return false
	}
}

func hasValue(r genev1alpha1.MatchRule, value string) bool {
	for i := range r.Values {
		if r.Values[i] == value {
			return true
		}
	}
	return false
}
