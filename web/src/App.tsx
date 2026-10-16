import { Route, Router } from "@solidjs/router";
import { Match, onMount, Switch } from "solid-js";
import { Toasts } from "./components/Feedback";
import { Shell } from "./layouts/Shell";
import { phase, refreshSession } from "./lib/state";
import { ApplicationPage } from "./pages/ApplicationPage";
import { ClusterPage } from "./pages/ClusterPage";
import { ClustersPage } from "./pages/Clusters";
import { LoginPage, SetupPage } from "./pages/Auth";
import { DashboardPage } from "./pages/Dashboard";
import { DatabasePage } from "./pages/DatabasePage";
import { DeploymentPage, DeploymentsPage } from "./pages/Deployments";
import { NewResourcePage } from "./pages/NewResource";
import { NotificationsPage } from "./pages/Notifications";
import { ProjectDetailPage, ProjectsPage } from "./pages/Projects";
import { SecurityPage } from "./pages/Security";
import { ServerDetailPage, ServersPage } from "./pages/Servers";
import { SettingsPage } from "./pages/Settings";
import { StackPage } from "./pages/StackPage";
import { NotFoundPage } from "./pages/NotFound";

export function App() {
  onMount(() => {
    refreshSession().catch(() => {
      // The backend is unreachable; the splash keeps showing and the user can reload.
    });
  });

  return (
    <>
      <Switch fallback={<div class="splash">Carregando o Pier…</div>}>
        <Match when={phase() === "setup"}>
          <SetupPage />
        </Match>
        <Match when={phase() === "anonymous"}>
          <LoginPage />
        </Match>
        <Match when={phase() === "authenticated"}>
          <Router root={Shell}>
            <Route path="/" component={DashboardPage} />
            <Route path="/projects" component={ProjectsPage} />
            <Route path="/projects/:id" component={ProjectDetailPage} />
            <Route path="/projects/:id/new" component={NewResourcePage} />
            <Route path="/applications/:id" component={ApplicationPage} />
            <Route path="/databases/:id" component={DatabasePage} />
            <Route path="/stacks/:id" component={StackPage} />
            <Route path="/deployments" component={DeploymentsPage} />
            <Route path="/deployments/:id" component={DeploymentPage} />
            <Route path="/servers" component={ServersPage} />
            <Route path="/servers/:id" component={ServerDetailPage} />
            <Route path="/clusters" component={ClustersPage} />
            <Route path="/clusters/:id" component={ClusterPage} />
            <Route path="/security" component={SecurityPage} />
            <Route path="/notifications" component={NotificationsPage} />
            <Route path="/settings" component={SettingsPage} />
            <Route path="*" component={NotFoundPage} />
          </Router>
        </Match>
      </Switch>
      <Toasts />
    </>
  );
}
