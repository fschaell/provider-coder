/*
Copyright 2022 The Crossplane Authors.

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

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/provider-coderworkspaces/apis/coder/v1alpha1"
	apisv1alpha1 "github.com/crossplane/provider-coderworkspaces/apis/v1alpha1"

	"github.com/crossplane/provider-coderworkspaces/internal/features"
	"github.com/go-resty/resty/v2"
)

const (
	errNotWorkspace = "managed resource is not a Workspace custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errNewClient = "cannot create new Service"
)

// A CoderService does nothing.
type CoderService struct {
	pCLI     *resty.Client
	token    string
	coderurl string
}

var (
	newCoderService = func(creds []byte, coderURL string) (*CoderService, error) {
		client := resty.New()

		return &CoderService{
			pCLI:     client,
			token:    string(creds),
			coderurl: coderURL,
		}, nil
	}
)

// Setup adds a controller that reconciles Workspace managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.WorkspaceGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.WorkspaceGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newCoderService}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1alpha1.Workspace{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         client.Client
	usage        resource.Tracker
	newServiceFn func(creds []byte, coderUrl string) (*CoderService, error)
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return nil, errors.New(errNotWorkspace)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}
	svc, err := c.newServiceFn(data, pc.Spec.CoderUrl)
	if err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}

	return &external{service: svc}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	// A 'client' used to connect to the external resource API. In practice this
	// would be something like an AWS SDK client.
	service *CoderService
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotWorkspace)
	}
	workspace_name := mg.GetName()

	// These fmt statements should be removed in the real implementation.
	fmt.Printf("Observing: %+v", cr)
	resp, _ := c.service.pCLI.
		SetHostURL(c.service.coderurl).
		R().EnableTrace().
		SetHeader("Accept", "application/json").
		SetHeader("Coder-Session-Token", c.service.token).
		Get("/api/v2/users/me/workspace/" + workspace_name)
	if resp.StatusCode() != 200 {
		return managed.ExternalObservation{
			ResourceExists:    false,
			ResourceUpToDate:  false,
			ConnectionDetails: managed.ConnectionDetails{},
		}, nil
	}

	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: true,

		// Return false when the external resource exists, but it not up to date
		// with the desired managed resource state. This lets the managed
		// resource reconciler know that it needs to call Update.
		ResourceUpToDate: true,

		// Return any details that may be required to connect to the external
		// resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotWorkspace)
	}

	var templates []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	// POST JSON string
	// No need to set content type, if you have client level setting

	fmt.Printf("Creating: %+v")

	username := strings.ReplaceAll(cr.Spec.ForProvider.UserName, ".", "")
	fmt.Printf("get User")
	var user struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`
		Organization_ids []string `json:"organization_ids"`
	}

	resp_user, _ := c.service.pCLI.
		SetHostURL(c.service.coderurl).
		R().EnableTrace().
		SetHeader("Accept", "application/json").
		SetHeader("Coder-Session-Token", c.service.token).
		Get("/api/v2/users/" + username)

	if err := json.Unmarshal(resp_user.Body(), &user); err != nil {
		return managed.ExternalCreation{}, fmt.Errorf("failed to decode user response: %w", err)
	}

	fmt.Printf("get Templates")
	resp_temp, _ := c.service.pCLI.
		SetHostURL(c.service.coderurl).
		R().EnableTrace().
		SetHeader("Accept", "application/json").
		SetHeader("Coder-Session-Token", c.service.token).
		Get("/api/v2/organizations/" + user.Organization_ids[0] + "/templates")

	//iterate over resp_temp and find the template_id
	fmt.Printf("get Templates")
	// Decode the response body into the templates slice
	if err := json.Unmarshal(resp_temp.Body(), &templates); err != nil {
		return managed.ExternalCreation{}, fmt.Errorf("failed to decode templates response: %w", err)
	}

	var templateID string
	for _, template := range templates {
		if template.Name == cr.Spec.ForProvider.Template {
			templateID = template.ID
			break
		}
	}

	if templateID == "" {
		return managed.ExternalCreation{}, errors.New("template not found")
	}

	resp, _ := c.service.pCLI.
		SetHostURL(c.service.coderurl).
		R().EnableTrace().
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("Coder-Session-Token", c.service.token).
		SetBody(`{"template_id":"` + templateID + `","name":"` + cr.GetName() + `"}`).
		//SetResult(&AuthSuccess{}).    // or SetResult(AuthSuccess{}).
		//SetError(&AuthError{}).       // or SetError(AuthError{}).
		Post("/api/v2/organizations/" + user.Organization_ids[0] + "/members/" + user.ID + "/workspaces")
	if resp.StatusCode() != 201 {
		return managed.ExternalCreation{}, fmt.Errorf("could not create workspace: %w", user.Name)
	}

	return managed.ExternalCreation{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotWorkspace)
	}

	fmt.Printf("Updating: %+v", cr)

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}

	fmt.Printf("Deleting: %+v", cr)

	username := strings.ReplaceAll(cr.Spec.ForProvider.UserName, ".", "")
	cmd := exec.Command("/usr/local/bin/coder", "delete", username+"/"+cr.GetName(), "--yes",
		"--url", c.service.coderurl, "--token", c.service.token)
	out, err := cmd.Output()
	if err != nil {
		// if there was any error, print it here
		fmt.Println("could not run command: ", err)
	}

	fmt.Println("Output: ", string(out))

	return nil
}
