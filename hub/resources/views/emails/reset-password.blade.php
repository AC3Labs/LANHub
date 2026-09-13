@extends('emails.layout')

@section('subject', 'Reset your '.config('app.name').' password')

@section('content')
<p style="margin:0 0 16px;">You requested a password reset. Click below to choose a new password:</p>

<table role="presentation" cellpadding="0" cellspacing="0" style="margin:24px 0;">
<tr>
<td style="border-radius:6px; background-color:#ac8544;">
    <a href="{{ $url }}" style="display:inline-block; padding:12px 24px; font-size:14px; font-weight:600; color:#ffffff; text-decoration:none;">Reset password</a>
</td>
</tr>
</table>

<p style="margin:0; font-size:13px; color:#867d6e;">If you didn't request this, no action is needed — your password will not be changed.</p>
@endsection
