# Changelog
All notable changes to this project will be documented in this file.

## [Unreleased]

## [1.2.0] - 2021-09-16
### Changed
* Updated Helm module to v3.7.0
* Updated AWS-SDK module to v1.40.43
* Updated AWS-Lambda module to v1.26.0
* Add cluster security group to Lambda 

## [1.1.1] - 2021-07-06
### Changed
* Updated Helm module to v3.6.2
* Fix for cross region ECR repositories 

## [1.1.0] - 2021-06-21
### Added
* Support for OCI repositories
* Support for authenticated pull from remote
### Changed
* Updated Helm module to v3.6.1 
* Updated Kubernetes module to v0.21.0
* Updated AWS SDK to v1.38.63
* Updated AWS Lambda SDK to v1.24.0
* Updated AWS IAM Authenticator to v0.5.3
  
## [1.0.1] - 2021-05-20
### Changed
* Remove unused IAM permissions
* Added retry while checking the resource for stabilization

## [1.0.0] - 2021-04-29
### Added
* Initial release of the `AWSQS::Kubernetes::Helm` resource provider