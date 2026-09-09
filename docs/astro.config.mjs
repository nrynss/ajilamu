// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

// https://astro.build/config
export default defineConfig({
  site: "https://nrynss.github.io",
  base: "/ajilamu",
  integrations: [
    starlight({
      title: "Ajilamu",
      description: "Give a film another tongue, and keep its rhythm.",
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/nrynss/ajilamu",
        },
      ],
      customCss: ["./src/styles/custom.css"],
      sidebar: [
        {
          label: "Getting Started",
          items: [
            { label: "Introduction", slug: "getting-started" },
            { label: "Quickstart", slug: "getting-started/quickstart" },
          ],
        },
        {
          label: "Core Architecture",
          items: [
            { label: "The Dubbing Pipeline", slug: "architecture/pipeline" },
            { label: "Two-Sided Fit Loop", slug: "architecture/fit-loop" },
            { label: "Immutable Ledger", slug: "architecture/ledger" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Cost & Economics", slug: "guides/cost-accounting" },
            { label: "Deployment & Production", slug: "guides/deployment" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "System Boundaries & Unbuilt Scope", slug: "reference/limitations" },
            { label: "Repository Map", slug: "reference/repository-map" },
            { label: "Agent Protocol & Principles", slug: "reference/protocol" },
          ],
        },
      ],
    }),
  ],
});
