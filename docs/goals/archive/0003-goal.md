## Goal 3 — GitHub & Azure DevOps as First-Class Engineering Connections

/goal Make **GitHub and Azure DevOps** first-class connected engineering systems in Symmetry.

At completion, a user must be able to connect GitHub and Azure DevOps accounts/organizations/projects with appropriately scoped credentials, inspect the resulting connections and their health, bind resources from those systems to a Symmetry project, and use their work-tracking and code-development data as native parts of the Symmetry experience.

GitHub support must cover the engineering objects needed for agent-driven development, including repositories, Issues where applicable, pull requests, review/merge state, and GitHub Actions/CI status.

Azure DevOps support must cover the corresponding engineering objects needed for agent-driven development, including Azure Boards Work Items, Azure Repos, pull requests, and Azure Pipelines status.

Symmetry must have a provider-neutral internal model for concepts such as project resource, work item, repository, change/pull request, CI status, and external connection. Provider-specific details may remain accessible, but GitHub and Azure DevOps must not leak provider-specific branching logic throughout the rest of the product.

External work trackers must be allowed to remain the source of truth. A team already using Azure DevOps or GitHub must not be required to migrate all work into a Symmetry-native issue tracker before agents can operate on it.

For externally owned work items, ownership of fields must be unambiguous. Provider-owned fields such as title, description, work-item state, tags/labels, and externally assigned humans should remain authoritative in the external system unless the configured integration explicitly establishes a different contract. Symmetry owns its own execution information such as agent assignment, run history, runtime ownership, execution lease/generation, agent policy, cost/usage, and generated artifacts. The system must avoid uncontrolled two-way mirroring in which two independent copies can both claim authority over the same field.

A Symmetry project must be able to bind multiple connected resources, for example an Azure DevOps project for work tracking, one or more GitHub repositories for code, and GitHub Actions or Azure Pipelines for CI.

Agents must be able to operate on connected work using narrowly scoped provider capabilities without unnecessarily exposing long-lived provider credentials to the coding-agent process. Connection and permission failures must be visible and diagnosable from the portal.

Completion is demonstrated by end-to-end flows for both GitHub and Azure DevOps in which connected work can appear in the Symmetry project/Kanban experience, be assigned to an agent, produce a linked code change/PR, surface review and CI state, and preserve the correct external-versus-Symmetry source-of-truth boundaries throughout the lifecycle.
