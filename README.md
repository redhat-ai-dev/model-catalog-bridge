# RHDH Model Catalog Bridge

This repository provides various containers that faciiltate theseamless export of AI model records from various AI Model Registres and imports them into Red Hat Developer Hub (Backstage) as catalog entities.

Current status: early POC stage.  
- We have some docker files in place for the container imaages, but more work is needed there
- This repository collaborates with Backstage catalog entensions currently hosted in [our fork of the RHDH plugins repository](https://github.com/redhat-ai-dev/rhdh-plugins/tree/main/workspaces/rhdh-ai).
- Until those plugins have assoicated images and can be added to OCP RHDH, we have to run those plugins, and by extension backstage, from our laptops.
- By extension, the `rhoai-normalizer` and `storage-reset` containers have to run on one's laptop as well.  The `location` container can run as an OCP deployment, but it is just as easy to run it out of your laptop as well.
- This [simple Gitops repo](https://github.com/redhat-ai-dev/odh-kubeflow-model-registry-setup) has the means of setting up Open Data Hub plus dev patches for Kubelow Model Registry that facilitate getting the URLs for running Models deployed into RHOAI/ODH by the Model Registry.


## Contributing

All contributions are welcome. The [Apache 2 license](http://www.apache.org/licenses/) is used and does not require any 
contributor agreement to submit patches. That said, the preference at this time for issue tracking is not GitHub issues
in this repository.  

Rather, visit the team's [RHDHPAI Jira project and the 'model-registry-bridge' component](https://issues.redhat.com/issues/?jql=project%20%3D%20RHDHPAI%20AND%20component%20%3D%20model-registry-bridge).

## Prerequisites

- An OpenShift cluster with 3x worker nodes, with at least 8 CPU, and 32GB memory each. 
   - on AWS `m6i.2xlarge` or `g5.2xlarge` (if GPUs are needed) work well
   - For other options, see https://aws.amazon.com/ec2/instance-types/

## Usage

Either via the command line, or from your favorite Golang editor, set the following environment variables as follows

### rhoai-normalizer

1. `K8S_TOKEN` - the login/bearer token of your `kubeadmin` user for the OCP cluster you are testing on
2. `KUBECONFIG` - the path to the local kubeconfig file corresponding to your OCP cluster
3. `MR_ROUTE` - the name of the Model Registry route in the `istio-system` namespace. For now, use `odh-model-registries-modelregistry-public-rest`.
4. `NAMESPACE` - the name of the namespace you create for deploying AI models from ODH
5. `STORAGE_URL` - for now, just use `http://localhost:7070`; this will be updated when we can run this container in OCP as part of the RHDH plugin running in RHDH
6. `NORMALIZER_FORMAT` - can either be `JsonArrayFormat` for our new format from the `schema` folder, or the legacy `CatalogInfoYamlFormat`; if not set defaults to `CatalogInfoYamlFormat` until RHDHPAI-611 and RHDHPAI-612 are completed.

### storage-rest

1. `RHDH_TOKEN` - the static token you create in backstage to allows for authenticated access to the Backstage catalog API.  See (https://github.com/redhat-ai-dev/rhdh-plugins/blob/main/workspaces/rhdh-ai/app-config.yaml#L19)[https://github.com/redhat-ai-dev/rhdh-plugins/blob/main/workspaces/rhdh-ai/app-config.yaml#L19]
2. `BKSTG_URL` - for now, just use `http://localhost:7007`; this will be updated when we can run this container in OCP as part of the RHDH plugin running in RHDH
3. `BRIDGE_URL` - for now, just use `http://localhost:9090`; this is the REST endpoint of our `location` container
4. `STORAGE_TYPE` - for now, only the development mode `ConfigMap` is supported; we'll add `GitHub` soon
5. `K8S_TOKEN`, `KUBECONFIG`, and `NAMESPACE` are the same as above
6. `NORMALIZER_FORMAT` - can either be `JsonArrayFormat` for our new format from the `schema` folder, or the legacy `CatalogInfoYamlFormat`; if not set defaults to `CatalogInfoYamlFormat` until RHDHPAI-611 and RHDHPAI-612 are completed.

### location

1. `STORAGE_URL` is the same as above
2. `NORMALIZER_FORMAT` - can either be `JsonArrayFormat` for our new format from the `schema` folder, or the legacy `CatalogInfoYamlFormat`; if not set defaults to `CatalogInfoYamlFormat` until RHDHPAI-611 and RHDHPAI-612 are completed.

When you are ready to launch the 3 processes, set your current namespace to the `NAMESPACE` value:

```
oc project $NAMESPACE
```

### Debugging (VS Code)

To debug the bridge services in VS Code, a launch.json file has been provided with options to debug each of the individual services. Ensure that you launch VS Code from the same terminal window that has the above environment variables set.

### Deploying a model

For our testing, we've been using the a cheap and relatively easy to deploy model the RHOAI team has posted on Hugging Face:  https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx

Use a "URL Connection" when registering in Model Registry and deploying from Model Registry.  ONNX is the model format.

Deploy the model in the `NAMESPACE` namespace noted above.  Also, in that namespace, creating the following `ServiceMeshMember` entry:

```
apiVersion: maistra.io/v1
kind: ServiceMeshMember
metadata:
  name: default
spec:
  controlPlaneRef:
    name: data-science-smcp
    namespace: istio-system
```

## Model Catalog Schema

The schema that the model catalog bridge will use to collect model and model server metadata can be found under [schema/](./schema/).

To re-generate the types that correspond to the schema, run `make generate-types-all`.
