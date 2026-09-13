@extends('emails.layout')

@section('subject', "You've been invited to ".config('app.name'))

@section('content')
<p style="margin:0 0 16px;">Hi {{ $name }},</p>
<p style="margin:0 0 16px;">You've been invited to {{ config('app.name') }} — a private file explorer for browsing and managing files across your machines. Set a password to activate your account:</p>

<table role="presentation" cellpadding="0" cellspacing="0" style="margin:24px 0;">
<tr>
<td style="border-radius:6px; background-color:#ac8544;">
    <a href="{{ $setPasswordUrl }}" style="display:inline-block; padding:12px 24px; font-size:14px; font-weight:600; color:#ffffff; text-decoration:none;">Set your password</a>
</td>
</tr>
</table>

<p style="margin:0; font-size:13px; color:#867d6e;">This link expires in 7 days. If you weren't expecting this invitation, you can ignore this email.</p>
@endsection
