# Model Catalog Schema

The goal of the [model catalog schema](./model-catalog.schema.json) is to provide a way to aggregate metadata for models and model servers into a consistent format that can be used to generate Backstage model catalog entities that represent them.

## Catalog Structure:
- Represent each model service as a `Component`
- Each deployed "model-server" `Component` dependsOn

    - 1 to N `Resources` representing the models deployed on the service
    - `API` representing the model server API type (e.g. OpenAI, OpenVINO, etc)
- Techdocs:

    - Techdoc for the `Component` referencing how to access the server
    - Techdoc for each model `Resource` providing information about the model, and any model-specific usage or restrictions

A reference model catalog schema can be found [here](https://github.com/redhat-ai-dev/model-catalog-example/blob/main/developer-model-service/catalog-info.yaml)

