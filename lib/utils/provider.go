package utils

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

// GetServiceProviderResource retrieves the ServiceProvider resource with the given name using the provided platform client.
func GetServiceProviderResource(ctx context.Context, platformClient client.Client, providerName string) (*v1alpha1.ServiceProvider, error) {
	serviceProvider := &v1alpha1.ServiceProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerName,
		},
	}
	if err := platformClient.Get(ctx, client.ObjectKeyFromObject(serviceProvider), serviceProvider); err != nil {
		return nil, fmt.Errorf("failed to get service provider resource %s: %w", providerName, err)
	}
	return serviceProvider, nil
}

// RegisterGVKsAtServiceProvider updates the status.resources field of the ServiceProvider resource with the provided GroupVersionKinds.
// This can be used by service providers, for example in their init job, to register the kinds of resources they manage.
func RegisterGVKsAtServiceProvider(ctx context.Context, platformClient client.Client, providerName string, gvks ...metav1.GroupVersionKind) error {
	providerResource, err := GetServiceProviderResource(ctx, platformClient, providerName)
	if err != nil {
		return err
	}

	providerResourceOld := providerResource.DeepCopy()
	providerResource.Status.Resources = gvks
	if err := platformClient.Status().Patch(ctx, providerResource, client.MergeFrom(providerResourceOld)); err != nil {
		return fmt.Errorf("failed to patch platform service provider status: %w", err)
	}

	return nil
}
