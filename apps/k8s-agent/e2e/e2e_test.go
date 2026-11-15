//go:build e2e

// Package e2e tests the agent against a real control plane and a kind
// cluster. run.sh starts them and sets the IVALDI_E2E_* variables; run it
// with make k8s_agent_e2e.
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	agentNamespace = "ivaldi-agent"
	appNamespace   = "e2e-demo"
	// The demo chart's overlay, and the annotation it sets from the
	// Environment's slug.
	overlay               = "values-e2e.yaml"
	environmentAnnotation = "e2e.ivaldi.build/environment"
	// The annotation the demo chart sets from the value the Application's
	// Greeting variable writes.
	greetingAnnotation = "e2e.ivaldi.build/greeting"
)

type settings struct {
	apiURL, clusterAPIURL      string
	adminEmail, adminPassword  string
	registry, chartDigest      string
	imageDigests               []string
	agentImage, agentChartPath string
}

func load(t *testing.T) settings {
	t.Helper()
	s := settings{
		apiURL: os.Getenv("IVALDI_E2E_API_URL"), clusterAPIURL: os.Getenv("IVALDI_E2E_CLUSTER_API_URL"),
		adminEmail: os.Getenv("IVALDI_E2E_ADMIN_EMAIL"), adminPassword: os.Getenv("IVALDI_E2E_ADMIN_PASSWORD"),
		registry: os.Getenv("IVALDI_E2E_REGISTRY"), chartDigest: os.Getenv("IVALDI_E2E_CHART_DIGEST"),
		imageDigests: strings.Split(os.Getenv("IVALDI_E2E_IMAGE_DIGESTS"), ","),
		agentImage:   os.Getenv("IVALDI_E2E_AGENT_IMAGE"), agentChartPath: os.Getenv("IVALDI_E2E_AGENT_CHART"),
	}
	if s.apiURL == "" {
		t.Skip("IVALDI_E2E_API_URL is unset; run apps/k8s-agent/e2e/run.sh")
	}
	require.Len(t, s.imageDigests, 2, "IVALDI_E2E_IMAGE_DIGESTS names two image digests")
	return s
}

// TestDeployAndRollBack connects a cluster as the onboarding guide does,
// deploys a Release whose chart runs an image, deploys a second Release, and
// then the first again, checking each time that the cluster runs exactly
// the Release's image and the overlay the placement selects.
//
// The first Release is deployed again as a rollout: the control plane does
// not request rollbacks until Variables and Secret Stores land, and without
// secrets the agent applies a rollback exactly as it does a rollout.
func TestDeployAndRollBack(t *testing.T) {
	s := load(t)
	ctx := context.Background()
	kube := kubeClient(t)
	cp := &controlPlane{t: t, base: s.apiURL}
	cp.token = cp.signIn(s.adminEmail, s.adminPassword)

	// The Workspace, Project, Environment, and Lifecycle a Release moves through.
	workspace := cp.list("/v1/workspaces")[0]
	team := cp.call(http.MethodPost, "/v1/teams", obj{"display_name": "E2E"})
	project := cp.call(http.MethodPost, "/v1/workspaces/"+id(workspace)+"/projects",
		obj{"display_name": "E2E", "slug": "e2e", "owner_team_id": id(team)})
	environment := cp.call(http.MethodPost, "/v1/workspaces/"+id(workspace)+"/environments",
		obj{"display_name": "E2E", "slug": "e2e", "tier": "development", "execution_enabled": true})
	lifecycle := cp.call(http.MethodPost, "/v1/workspaces/"+id(workspace)+"/lifecycles", obj{
		"display_name": "E2E",
		"phases":       []obj{{"name": "E2E", "environment_ids": []string{id(environment)}}},
	})

	// Connect the cluster: one install, enrolled with a bearer token.
	cluster := cp.call(http.MethodPost, "/v1/clusters", obj{"display_name": "kind", "runtime": "kubernetes"})
	enrolment := cp.call(http.MethodPost, "/v1/clusters/"+id(cluster)+"/enrolments", obj{"method": "bearer"})
	for _, ns := range []string{agentNamespace, appNamespace} {
		_, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	_, err := kube.CoreV1().Secrets(agentNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ivaldi-enrolment"},
		StringData: map[string]string{"token": str(t, enrolment, "token")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	repository, tag, _ := strings.Cut(s.agentImage, ":")
	helm(t, "install", "ivaldi-agent", s.agentChartPath, "--namespace", agentNamespace,
		"--set", "controlPlane.url="+s.clusterAPIURL,
		"--set", "controlPlane.allowInsecureHTTP=true",
		"--set", "agent.authentication=bearer",
		"--set", "execute.namespaces={"+appNamespace+"}",
		"--set", "registry.plainHTTPHosts={"+s.registry+"}",
		"--set", "image.repository="+repository,
		"--set", "image.tag="+tag,
		"--set", "image.pullPolicy=Never",
	)

	// A Target in the cluster, which the agent reports it can deploy to.
	target := cp.call(http.MethodPost, "/v1/environments/"+id(environment)+"/targets", obj{
		"display_name": "Demo", "runtime": "kubernetes",
		"locator": obj{"cluster": str(t, cluster, "slug"), "namespace": appNamespace},
	})
	eventually(t, 3*time.Minute, func() (bool, string) {
		got := cp.call(http.MethodGet, "/v1/targets/"+id(target), nil)
		return got["can_execute"] == true, fmt.Sprintf("can_execute=%v reason=%v", got["can_execute"], got["capability_reason"])
	})

	// An Application with a chart and the image it runs, placed on the Target.
	application := cp.call(http.MethodPost, "/v1/projects/"+id(project)+"/applications",
		obj{"display_name": "Demo", "slug": "demo", "default_lifecycle_id": id(lifecycle)})
	if inner, ok := application["application"].(obj); ok {
		application = inner
	}
	app := id(application)
	cp.call(http.MethodPost, "/v1/applications/"+app+"/artifacts",
		obj{"name": "chart", "artifact_kind": "helm_chart", "artifact_name": s.registry + "/charts/demo", "required": true})
	cp.call(http.MethodPost, "/v1/applications/"+app+"/artifacts",
		obj{"name": "app", "artifact_kind": "container_image", "artifact_name": s.registry + "/demo", "required": true})
	cp.call(http.MethodPost, "/v1/applications/"+app+"/placements", obj{
		"target_id":    id(target),
		"apply_config": obj{"definition_artifact": "chart", "values": []string{overlay}},
	})

	// A variable the chart receives at `greeting`: one value for every
	// Deployment, and a more specific one for this Environment that binds a
	// system variable. A Release deploys with the values it was created with.
	slug := str(t, environment, "slug")
	variable := cp.call(http.MethodPost, "/v1/applications/"+app+"/variables", obj{
		"name": "Greeting", "values_path": "greeting",
		"values": []obj{
			{"value": "hello"},
			{"value": "hello from #{Ivaldi.Environment.Slug}", "scope": obj{"environment_ids": []string{id(environment)}}},
		},
	})
	scopedValue := id(variable["values"].([]any)[1].(obj))

	release := func(version, imageDigest string) string {
		r := cp.call(http.MethodPost, "/v1/applications/"+app+"/releases", obj{
			"version": version, "publish": true,
			"builds": []obj{
				{"artifact": "chart", "digest": s.chartDigest, "version": "0.1.0"},
				{"artifact": "app", "digest": imageDigest, "version": version},
			},
		})
		return id(r)
	}
	deploy := func(releaseID, imageDigest, greeting string) {
		t.Helper()
		requested := cp.callList(http.MethodPost, "/v1/releases/"+releaseID+"/deployments", obj{"target_ids": []string{id(target)}})
		require.Len(t, requested, 1)
		deployment := id(requested[0])
		eventually(t, 4*time.Minute, func() (bool, string) {
			got := cp.call(http.MethodGet, "/v1/deployments/"+deployment, nil)
			if d, ok := got["deployment"].(obj); ok {
				got = d
			}
			status, _ := got["status"].(string)
			require.NotContains(t, []string{"failed", "cancelled", "rejected"}, status, "deployment %s: %v", deployment, got)
			return status == "healthy", "deployment " + deployment + " is " + status
		})
		want := s.registry + "/demo@" + imageDigest
		running := runningImage(t, kube, app)
		assert.Equal(t, want, running.image, "the workload runs the Release's image")
		assert.Equal(t, slug, running.annotation, "the placement's overlay is applied, with its binding resolved")
		assert.Equal(t, greeting, running.greeting, "the variable's most specific value, as the Release snapshotted it, with its binding resolved")
	}

	first, second := s.imageDigests[0], s.imageDigests[1]
	v1 := release("1.0.0", first)
	deploy(v1, first, "hello from "+slug)

	// The variable changes after 1.0.0 was created: 2.0.0 gets the new value,
	// and deploying 1.0.0 again brings its own back.
	cp.call(http.MethodPut, "/v1/variables/"+id(variable)+"/values/"+scopedValue, obj{
		"value": "hi from #{Ivaldi.Environment.Slug}", "scope": obj{"environment_ids": []string{id(environment)}},
	})
	deploy(release("2.0.0", second), second, "hi from "+slug)
	deploy(v1, first, "hello from "+slug)
}

type running struct{ image, annotation, greeting string }

// runningImage returns the image of the Application's workload once its
// rollout is complete, and the annotation the overlay sets.
func runningImage(t *testing.T, kube kubernetes.Interface, applicationID string) running {
	t.Helper()
	var out running
	eventually(t, 2*time.Minute, func() (bool, string) {
		list, err := kube.AppsV1().Deployments(appNamespace).List(context.Background(),
			metav1.ListOptions{LabelSelector: "ivaldi.build/application=" + applicationID})
		require.NoError(t, err)
		if len(list.Items) != 1 {
			return false, fmt.Sprintf("%d workloads carry the Application's label", len(list.Items))
		}
		d := list.Items[0]
		if !rolledOut(&d) {
			return false, "the workload is still rolling out"
		}
		out = running{image: d.Spec.Template.Spec.Containers[0].Image, annotation: d.Annotations[environmentAnnotation], greeting: d.Annotations[greetingAnnotation]}
		return true, ""
	})
	return out
}

func rolledOut(d *appsv1.Deployment) bool {
	want := int32(1)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	s := d.Status
	return s.ObservedGeneration >= d.Generation && s.UpdatedReplicas == want && s.AvailableReplicas == want && s.Replicas == want
}

func kubeClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	require.NoError(t, err)
	kube, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	return kube
}

func helm(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("helm", args...).CombinedOutput()
	require.NoError(t, err, "helm %s: %s", strings.Join(args, " "), out)
}

// eventually polls check every two seconds until it passes, failing with
// its last message when timeout runs out.
func eventually(t *testing.T, timeout time.Duration, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, msg := check()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s: %s", timeout, msg)
		}
		time.Sleep(2 * time.Second)
	}
}

type obj = map[string]any

func id(o obj) string {
	v, _ := o["id"].(string)
	if v == "" {
		panic(fmt.Sprintf("no id in %v", o))
	}
	return v
}

func str(t *testing.T, o obj, key string) string {
	t.Helper()
	v, ok := o[key].(string)
	require.True(t, ok, "no %s in %v", key, o)
	return v
}

// controlPlane calls the control plane's API as the bootstrap admin.
type controlPlane struct {
	t     *testing.T
	base  string
	token string
}

// signIn signs in as ivaldictl does, playing both the browser and the web
// app: authorize with PKCE, sign in with the password, and redeem the code.
func (c *controlPlane) signIn(email, password string) string {
	c.t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(c.t, err)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	redirect := func(target string) *url.URL {
		resp, err := browser.Get(target)
		require.NoError(c.t, err)
		resp.Body.Close()
		require.Equal(c.t, http.StatusFound, resp.StatusCode, target)
		u, err := url.Parse(resp.Header.Get("Location"))
		require.NoError(c.t, err)
		return u
	}

	verifier := rand.Text() + rand.Text()
	sum := sha256.Sum256([]byte(verifier))
	const redirectURI = "http://127.0.0.1:9/callback"
	login := redirect(c.base + "/oauth/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {"ivaldictl"}, "redirect_uri": {redirectURI}, "state": {"e2e"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode())
	step := c.raw(http.MethodPost, "/v1/auth/login/password", obj{
		"login_challenge": login.Query().Get("login_challenge"), "email": email, "password": password,
	}, http.StatusOK)
	next := step["redirect_to"].(string)
	if strings.HasPrefix(next, "/") {
		next = c.base + next
	}
	code := redirect(next).Query().Get("code")
	require.NotEmpty(c.t, code)

	resp, err := http.PostForm(c.base+"/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"ivaldictl"}, "code": {code},
		"redirect_uri": {redirectURI}, "code_verifier": {verifier},
	})
	require.NoError(c.t, err)
	defer resp.Body.Close()
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(c.t, json.NewDecoder(resp.Body).Decode(&tokens))
	require.NotEmpty(c.t, tokens.AccessToken)
	return tokens.AccessToken
}

// call makes a request expecting success and returns its data object.
func (c *controlPlane) call(method, path string, body any) obj {
	c.t.Helper()
	data, ok := c.success(method, path, body).(obj)
	require.True(c.t, ok, "%s %s: data is not an object", method, path)
	return data
}

// callList makes a request expecting success and returns its data list.
func (c *controlPlane) callList(method, path string, body any) []obj {
	c.t.Helper()
	data := c.success(method, path, body)
	if page, ok := data.(obj); ok {
		data = page["items"]
	}
	items, ok := data.([]any)
	require.True(c.t, ok, "%s %s: data is not a list", method, path)
	out := make([]obj, len(items))
	for i, item := range items {
		out[i] = item.(obj)
	}
	return out
}

func (c *controlPlane) list(path string) []obj {
	c.t.Helper()
	return c.callList(http.MethodGet, path, nil)
}

func (c *controlPlane) success(method, path string, body any) any {
	c.t.Helper()
	resp := c.raw(method, path, body, 0)
	return resp["data"]
}

// raw makes a request and decodes the response; status 0 accepts any 2xx.
func (c *controlPlane) raw(method, path string, body any, status int) obj {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	ok := resp.StatusCode == status || (status == 0 && resp.StatusCode/100 == 2)
	require.True(c.t, ok, "%s %s: %d %s", method, path, resp.StatusCode, raw)
	var out obj
	if len(raw) > 0 {
		require.NoError(c.t, json.Unmarshal(raw, &out), "%s %s: %s", method, path, raw)
	}
	return out
}
