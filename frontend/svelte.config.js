import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	kit: {
		adapter: adapter({
			pages: '../backend/priv/static',
			assets: '../backend/priv/static',
			fallback: 'index.html'
		})
	}
};

export default config;
