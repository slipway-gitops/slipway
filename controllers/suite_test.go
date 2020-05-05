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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/lithammer/shortuuid"
	gitv1 "github.com/slipway-gitops/slipway/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg            *rest.Config
	k8sClient      client.Client
	testEnv        *envtest.Environment
	stopCh         chan struct{}
	gitcontrol     *GitRepoReconciler
	hashcontrol    *HashReconciler
	gittestlogger  *TestLogger
	hashtestlogger *TestLogger
	scheme         = runtime.NewScheme()
)

var (
	ErrChannelTimeOut = errors.New("Timed out waiting for log entry")
)

func SetupTestEnv() error {
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}
	var err error
	cfg, err = testEnv.Start()
	if err != nil {
		return err
	}
	err = clientgoscheme.AddToScheme(scheme)
	if err != nil {
		return err
	}
	err = gitv1.AddToScheme(scheme)
	if err != nil {
		return err
	}
	// +kubebuilder:scaffold:scheme
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	if k8sClient == nil {
		return errors.New("Nil k8s client")
	}
	return nil
}

func SetupTestGitRepo() error {
	stopCh = make(chan struct{})
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		return err
	}
	gittestlogger = &TestLogger{Timeout: 20000000000, Logs: make(chan log, 500)}
	gitcontrol = &GitRepoReconciler{
		Client:     mgr.GetClient(),
		Log:        gittestlogger.WithName("controllers").WithName("GitRepo"),
		recorder:   mgr.GetEventRecorderFor("gitrepo-controller"),
		Scheme:     mgr.GetScheme(),
		PluginPath: "../internal/bin/",
	}
	err = gitcontrol.SetupWithManager(mgr)
	if err != nil {
		return err
	}
	go mgr.Start(stopCh)
	return nil
}

func SetupTestHash() error {
	stopCh = make(chan struct{})
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		return err
	}

	hashtestlogger = &TestLogger{Timeout: 20000000000, Logs: make(chan log, 500)}
	hashcontrol = &HashReconciler{
		Client:     mgr.GetClient(),
		Log:        hashtestlogger.WithName("controllers").WithName("Hash"),
		recorder:   mgr.GetEventRecorderFor("hash-controller"),
		Scheme:     mgr.GetScheme(),
		PluginPath: "../internal/bin/",
	}
	err = hashcontrol.SetupWithManager(mgr)
	if err != nil {
		return err
	}
	go mgr.Start(stopCh)
	return nil
}

func TearDownTestGitRepo() {
	close(stopCh)
	close(gittestlogger.Logs)
}

func TearDownTestHash() {
	close(stopCh)
	close(hashtestlogger.Logs)
}

func TearDownTestEnv() error {
	return testEnv.Stop()
}

type log struct {
	logtype string
	message string
}

type logDumpError struct {
	logs []log
}

func (ld logDumpError) Error() string {
	var logs string
	for _, v := range ld.logs {
		logs += fmt.Sprintln(v)
	}
	return logs
}

type TestLogger struct {
	Logs    chan log
	Timeout time.Duration
}

func (t *TestLogger) ReadUntilType(v string) (entry string, err error) {
	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(t.Timeout)
		timeout <- true
	}()
	var logDump []log
	for {
		select {
		case l := <-t.Logs:
			if l.logtype == v {
				return l.message, nil
			}
			logDump = append(logDump, l)
			continue

		case <-timeout:
			return "", logDumpError{logs: logDump}
		}
	}
}

func (t *TestLogger) ReadUntilRegex(v string, message string) error {
	timeout := make(chan bool, 1)
	regex, err := regexp.Compile(message)
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(t.Timeout)
		timeout <- true
	}()
	var logDump []log
	for {
		select {
		case l := <-t.Logs:
			if l.logtype == v && regex.Match([]byte(l.message)) {
				return nil
			}
			logDump = append(logDump, l)
			continue

		case <-timeout:
			return logDumpError{logs: logDump}
		}
	}

}

func (t *TestLogger) ReadUntilLog(v string, entry string) error {
	timeout := make(chan bool, 1)
	testlog := log{
		logtype: v,
		message: entry,
	}
	go func() {
		time.Sleep(t.Timeout)
		timeout <- true
	}()
	var logDump []log
	for {
		select {
		case l := <-t.Logs:
			if reflect.DeepEqual(testlog, l) {
				return nil
			}
			logDump = append(logDump, l)
			continue

		case <-timeout:
			return logDumpError{logs: logDump}
		}
	}
}

func (t *TestLogger) writer(v string, entry string) {
	defer func() {
		recover()
	}()
	l := log{
		logtype: v,
		message: entry,
	}
	t.Logs <- l
}

func (t *TestLogger) Info(v string, args ...interface{}) {
	go t.writer("info", fmt.Sprintf("%s: %v", v, args))
}

func (t *TestLogger) Enabled() bool {
	go t.writer("enabled", "")
	return true
}

func (t *TestLogger) Error(err error, msg string, args ...interface{}) {
	go t.writer("error", fmt.Sprintf("%s: %v -- %v", msg, err, args))
}

func (t *TestLogger) V(v int) logr.InfoLogger {
	return t
}

func (t *TestLogger) WithName(name string) logr.Logger {
	go t.writer("name", name)
	return t
}

func (t *TestLogger) WithValues(args ...interface{}) logr.Logger {
	for _, v := range args {
		go t.writer("value", fmt.Sprint(v))
	}
	return t
}

func (t *TestLogger) Empty() {
	for len(t.Logs) > 0 {
		<-t.Logs
	}
}

func NewGitHandler(r *gitv1.GitRepo) GitHandler {
	return GitHandler{
		Object: r,
		ctx:    context.TODO(),
	}
}

type GitHandler struct {
	Object *gitv1.GitRepo
	ctx    context.Context
}

func (g *GitHandler) Create() error {
	if g.Object.GetName() != "" {
		err := g.Clean()
		if err != nil {
			return err
		}
	}
	g.Object.ObjectMeta = metav1.ObjectMeta{
		Name: (strings.ToLower(shortuuid.New())),
	}
	return k8sClient.Create(g.ctx, g.Object)
}

func (g *GitHandler) Clean() error {
	return k8sClient.Delete(g.ctx, g.Object)
}

func (g *GitHandler) Name() string {
	return g.Object.GetName()
}

func (g *GitHandler) NamespacedName() string {
	return fmt.Sprintf("%s/%s", g.Object.GetNamespace(), g.Object.GetName())
}

func (g *GitHandler) Get() error {
	u := &gitv1.GitRepo{}
	return k8sClient.Get(g.ctx, client.ObjectKey{Name: g.Object.ObjectMeta.Name}, u)
}

func NewHashHandler(r *gitv1.Hash) HashHandler {
	return HashHandler{
		Object: r,
		ctx:    context.TODO(),
	}
}

type HashHandler struct {
	Object *gitv1.Hash
	ctx    context.Context
}

func (g *HashHandler) Create() error {
	if g.Object.GetName() != "" {
		err := g.Clean()
		if err != nil {
			return err
		}
	}
	g.Object.ObjectMeta = metav1.ObjectMeta{
		Name: (strings.ToLower(shortuuid.New())),
	}
	return k8sClient.Create(g.ctx, g.Object)
}

func (g *HashHandler) Clean() error {
	return k8sClient.Delete(g.ctx, g.Object)
}

func (g *HashHandler) Name() string {
	return g.Object.GetName()
}

func (g *HashHandler) NamespacedName() string {
	return fmt.Sprintf("%s/%s", g.Object.GetNamespace(), g.Object.GetName())
}

func (g *HashHandler) Get() error {
	u := &gitv1.Hash{}
	return k8sClient.Get(g.ctx, client.ObjectKey{Name: g.Object.ObjectMeta.Name}, u)
}
