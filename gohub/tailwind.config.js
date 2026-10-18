// The PHP hub's Tailwind config (same colours, dark mode, fonts), scanning
// the Go hub's templates and scripts instead of Blade views.
import hub from '../hub/tailwind.config.js';

export default { ...hub, content: ['./web/templates/*.html', './web/static/lanhub.js'] };
