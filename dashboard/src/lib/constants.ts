/** Catálogo fixo de tecnologias suportadas no cadastro de apps.
 *  Manter em ordem alfabética por categoria para navegação simples.
 *  Novas entradas: adicione aqui — o backend não valida, aceita qualquer
 *  string (a lista serve para consistência de UI e telemetria agregada).
 */
export const TECHNOLOGIES: readonly string[] = [
  // Web frameworks (SPA/SSR)
  'React',
  'Next.js',
  'Vue.js',
  'Nuxt.js',
  'Angular',
  'Svelte',
  'SvelteKit',
  'Solid.js',
  'Remix',
  'Astro',
  'Ember.js',
  // Backend / APIs
  'Node.js',
  'Express',
  'Fastify',
  'NestJS',
  'Deno',
  'Bun',
  'Go',
  'Java',
  'Spring Boot',
  'Kotlin',
  '.NET / ASP.NET',
  'Ruby on Rails',
  'Django',
  'Flask',
  'FastAPI',
  'Laravel',
  'PHP',
  'Elixir / Phoenix',
  // Mobile
  'React Native',
  'Flutter',
  'Ionic',
  'iOS (Swift)',
  'Android (Kotlin)',
  // Outros
  'Vanilla JS',
  'WordPress',
  'Outro',
] as const;
