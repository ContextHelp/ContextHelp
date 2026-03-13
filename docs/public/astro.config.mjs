// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
	site: 'https://context.help',
	base: '/docs',
	integrations: [
		starlight({
			title: 'CTXt User Manual',
			description: 'Context-driven development knowledge management',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/ideacrafterslabs/ctxt' }],
			sidebar: [
				{
					label: 'Start Here',
					slug: '',
				},
				{
					label: 'Personas',
					autogenerate: { directory: 'personas' },
				},
				{
					label: 'Workflows',
					autogenerate: { directory: 'workflows' },
				},
				{
					label: 'Admin & Extensibility',
					autogenerate: { directory: 'admin-extensibility' },
				},
				{
					label: 'Operations',
					autogenerate: { directory: 'operations' },
				},
				{
					label: 'Reference',
					autogenerate: { directory: 'reference' },
				},
				{
					label: 'Troubleshooting',
					autogenerate: { directory: 'troubleshooting' },
				},
				{
					label: 'Appendix',
					autogenerate: { directory: 'appendix' },
				},
			],
		}),
	],
});
