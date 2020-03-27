/*

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

package main

import (
	"bytes"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awss3 "github.com/aws/aws-sdk-go/service/s3"
	"github.com/slipway-gitops/slipway/pkg/objectstore"
)

var (
	ObjectStore s3
)

type s3 struct {
	bucket string
	svc    *awss3.S3
}

func (s s3) Save(hash, operation string, yaml []byte) error {
	key := fmt.Sprintf("%s/%s/%v", hash, operation, time.Now().Unix())
	input := &awss3.PutObjectInput{
		Body:   aws.ReadSeekCloser(bytes.NewReader(yaml)),
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	_, err := s.svc.PutObject(input)
	return err

}

func (s s3) New(bucket string) objectstore.ObjectStore {
	s.svc = awss3.New(session.New())
	s.bucket = bucket
	return s
}
