# Deployment Controllers

An OpenMCP landscape has three controllers (called _deployment controllers_) which are responsible for deploying other controllers (called _providers_):

- the **ClusterProvider Controller** is responsible for deploying cluster providers. 
- the **ServiceProvider Controller** is responsible for deploying service providers.
- the **PlatformService Controller** is responsible for deploying platform services.

The deployments are specified in kubernetes resources of the kinds `ClusterProvider`, `ServiceProvider`, and `PlatformService` respectively.

## Provider Image

To be deployable, each provider must have an image available in a container registry. The image must have an executable as entrypoint. It will be used twice: to initialize the provider and to run it. For the initialization, a Job is started with the executable, and the following arguments are supplied:

```shell
init
--environment <environment>
--verbosity <DEBUG, INFO, or ERROR>
```

Once the initialization job has completed, a Deployment is created/updated with the same image and the following arguments:

```shell
run
--environment <environment>
--verbosity <DEBUG|INFO|ERROR>
```

## Provider Resource

The provider resources specify how to deploy the providers. They are of the kind `ClusterProvider`, `ServiceProvider`, or `PlatformService`. They are cluster-scoped, and have the following common structure:

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: <ClusterProvider|ServiceProvider|PlatformService>
metadata:
  name: <name>
spec:
  image: <image>
  imagePullSecrets:
    - name: <image-pull-secret-name>
  env:
    - name: <environment-variable-name>
      value: <environment-variable-value>
  verbosity: <DEBUG|INFO|ERROR>
```

- The `image` field specifies the container image to use for the init job and deployment of the provider. 
- The `imagePullSecrets` field specifies a list of secrets that contain the credentials to pull the image from a registry. 
- The `env` field specifies a list of name-value pairs that are passed as environment variables to the init job and deployment of the provider.
- The `verbosity` field specifies the logging level. Supported values are DEBUG, INFO, and ERROR. The default is INFO.
