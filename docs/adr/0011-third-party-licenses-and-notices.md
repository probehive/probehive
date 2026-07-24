# 0011: Third-Party Licenses and Notices

- Status: Accepted
- Date: 2026-07-23
- Clarifies: [ADR 0001](0001-license-and-open-core-boundary.md)

## Context

The public repository is Apache-2.0 for original ProbeHive work, but dependencies, fonts, images, generated material, protocol data, and other independently sourced artifacts may carry their own copyright and license terms. A notice does not relicense third-party work.

## Decision

License original ProbeHive source, documentation, contracts, generated clients owned by the project, and deployment assets under Apache-2.0 unless a deliberately reviewed file states another owner-approved license.

Retain each third-party artifact under its original compatible license, copyright statement, attribution, and required notices. Record its source, exact version or revision, license, and material obligations in the repository notice process. Do not replace a third-party license with Apache-2.0 merely because the artifact is stored in this repository.

Do not include an artifact whose redistribution, modification, patent, attribution, source-availability, trademark, or field-of-use terms are incompatible with the intended public distribution without owner and legal approval. Prefer dependencies fetched through reviewed package or build tooling over copied source or binary assets.

Trademark policy governs use of the ProbeHive marks and does not silently change the copyright license of source or artwork. Any ProbeHive visual asset that is not Apache-2.0 licensed states its copyright license explicitly in addition to the trademark policy.

## Consequences

- The root Apache-2.0 license remains clear for original project work.
- Third-party obligations remain visible and are not reduced to an ambiguous notice.
- New assets and generated material require provenance and license review before commit.
- Legal review remains required before the first public release and external contributions.
