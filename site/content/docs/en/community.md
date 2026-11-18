## License and access

Pier is **source-available**, under the **Apache-2.0 license with the Commons Clause**. In practice:

- **Allowed, without asking or paying:** using it in your company, including for commercial purposes; deploying your own systems with it and those you build for customers, as long as they do not receive access to Pier; studying, modifying, forking and sharing it free of charge.
- **Requires a commercial license, with royalties:** selling Pier or something whose value comes from it, even with only a few changes. That includes resale, a version under another brand (*white label*), a paid hosted or managed panel, and paid support or consulting about Pier itself.

It is not "open source" as the OSI defines it, because selling it requires a commercial license. Versions up to and including 0.9.0 remain under plain Apache-2.0. Read [LICENSE]({{repo}}/blob/main/LICENSE), [COMMERCIAL]({{repo}}/blob/main/COMMERCIAL.md), [NOTICE]({{repo}}/blob/main/NOTICE) and the dependency notices before redistributing.

## Contributing and reporting problems

See [CONTRIBUTING]({{repo}}/blob/main/CONTRIBUTING.md), [GOVERNANCE]({{repo}}/blob/main/GOVERNANCE.md) and the [code of conduct]({{repo}}/blob/main/CODE_OF_CONDUCT.md). For vulnerabilities, follow [SECURITY]({{repo}}/blob/main/SECURITY.md) and avoid publishing secrets in issues.

Automated testing, releases and updates reduce routine work. They do not remove human review, responsibility for security or the decision to accept changes. Automatic checks do not guarantee that a service is correct in production. Confirm permissions, branch protection and CI results before trusting automatic flows.

## Beta and operation

The code version is **{{version}}**, read directly from the server when this site is generated. The site does not change that number to announce features. The PIER 0.6 line is still beta: validate the flows you need and test recovery in a separate environment. Updates are applied by the operator; the panel does not replace itself. See [upgrading](/en/docs/upgrading/).
