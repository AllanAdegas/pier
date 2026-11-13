import { A } from "@solidjs/router";
import { Icon } from "../../components/Icon";
import { t } from "../../i18n";

/** Entry points of cloud provisioning and log forwarding on the server list. */
export function ServerExtrasLinks() {
  return (
    <div class="row">
      <A href="/infrastructure/cloud" class="btn btn-sm">
        <Icon name="globe" size={16} />
        {t("Criar servidor na nuvem")}
      </A>
      <A href="/infrastructure/log-drains" class="btn btn-sm">
        <Icon name="lines" size={16} />
        {t("Encaminhamento de logs")}
      </A>
    </div>
  );
}
