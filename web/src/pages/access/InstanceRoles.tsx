import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";
import { Select } from "../../components/Form";
import type { Role } from "./can";
import { roleLabels } from "./roleLabels";

export type InstanceMember = { user_id: string; email: string; role: Role | "" };

/** Instance roles of every user; only owners may read or change them (the API enforces it). */
export function createInstanceRoles(enabled: () => boolean) {
  const members = createQuery(() => (enabled() ? true : undefined), () => api.get<InstanceMember[]>("/access/instance"));
  const change = createAction((userId: string, role: string) =>
    api.put(`/access/instance/${encodeURIComponent(userId)}`, { role }), {
    success: t("Alteração salva."),
    onSuccess: () => void members.refetch(),
  });
  const roleOf = (userId: string) => members.data()?.find((member) => member.user_id === userId)?.role;
  return { members, change, roleOf };
}

export function instanceRoleOptions() {
  const labels = roleLabels();
  return [
    { value: "owner", label: labels.owner },
    { value: "admin", label: labels.admin },
    { value: "developer", label: labels.developer },
    { value: "viewer", label: labels.viewer },
    { value: "", label: t("Somente projetos") },
  ];
}

export function InstanceRoleSelect(props: { userId: string; role: Role | ""; pending: boolean; onChange: (role: string) => void }) {
  return (
    <div class="inline-select">
      <Select
        label={t("Papel na instância")}
        value={props.role}
        disabled={props.pending}
        options={instanceRoleOptions()}
        onValue={props.onChange}
      />
    </div>
  );
}
