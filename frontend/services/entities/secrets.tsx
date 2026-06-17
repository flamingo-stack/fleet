import { ISecret, ISecretPayload } from "interfaces/secrets";
import sendRequest from "services";
import { buildQueryStringFromParams } from "utilities/url";
import endpoints from "utilities/endpoints";

export interface IListSecretsRequestApiParams {
  page?: number;
  per_page?: number;
}

export interface IListSecretsResponse {
  custom_variables: ISecret[] | null;
  meta: {
    has_next_results: boolean;
    has_previous_results: boolean;
  };
  count: number;
}

export default {
  getSecrets(
    params: IListSecretsRequestApiParams
  ): Promise<IListSecretsResponse> {
    const { VARIABLES } = endpoints;
    const path = `${VARIABLES}?${buildQueryStringFromParams({
      page: params.page,
      per_page: params.per_page,
    })}`;

    return sendRequest("GET", path);
  },

  addSecret(secret: ISecretPayload) {
    const { VARIABLES } = endpoints;
    return sendRequest("POST", VARIABLES, secret);
  },

  deleteSecret(secretId: number) {
    const { VARIABLES } = endpoints;
    return sendRequest("DELETE", `${VARIABLES}/${secretId}`);
  },
};
